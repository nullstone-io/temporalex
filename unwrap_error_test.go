package temporalex

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"net/url"
	"testing"
	"time"
)

var _ ErrorWrapper = customErrorType{}

type customErrorType struct {
	Extra string `json:"extra"`
}

func (e customErrorType) WrapError() error {
	return temporal.NewApplicationErrorWithCause("custom error type", "customErrorType", e, e)
}

func (e customErrorType) Error() string {
	return fmt.Sprintf("custom error type: %s", e.Extra)
}

func TestUnwrapError_Activity(t *testing.T) {
	tests := map[string]struct {
		fn             func(ctx context.Context) error
		wantErrType    UnwrapErrType
		wantErrMessage string
		wantErr        error
	}{
		"panic": {
			fn: func(ctx context.Context) error {
				var x *url.URL
				x.String() // Intentional nil panic
				return nil
			},
			wantErrType:    UnwrapErrTypePanic,
			wantErrMessage: "runtime error: invalid memory address or nil pointer dereference",
			wantErr:        PanicError{Message: "runtime error: invalid memory address or nil pointer dereference"},
		},
		"cancellation": {
			fn: func(ctx context.Context) error {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				return temporal.NewCanceledError(ctx.Err().Error())
			},
			wantErrType:    UnwrapErrTypeCancellation,
			wantErrMessage: context.Canceled.Error(),
			wantErr:        temporal.NewCanceledError(context.Canceled.Error()),
		},
		"timeout": {
			fn: func(ctx context.Context) error {
				ctx, cancel := context.WithDeadline(ctx, time.Now())
				defer cancel()
				time.Sleep(time.Nanosecond)
				return temporal.NewCanceledError(ctx.Err().Error())
			},
			wantErrType:    UnwrapErrTypeCancellation,
			wantErrMessage: context.DeadlineExceeded.Error(),
			wantErr:        temporal.NewCanceledError(context.DeadlineExceeded.Error()),
		},
		"custom-error": {
			fn: func(ctx context.Context) error {
				return WrapCustomError(customErrorType{Extra: "extra"})
			},
			wantErrType:    UnwrapErrTypeFailure,
			wantErrMessage: "custom error type: extra",
			wantErr: customErrorType{
				Extra: "extra",
			},
		},
		"anonymous-error": {
			fn: func(ctx context.Context) error {
				return fmt.Errorf("anonymous error message")
			},
			wantErrType:    UnwrapErrTypeFailure,
			wantErrMessage: "anonymous error message",
			wantErr:        errors.New("anonymous error message"),
		},
	}

	RegisterCustomErrorDefault[customErrorType]("customErrorType")

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			scaffold := ActivityTestScaffold{T: t}
			scaffold.Env = scaffold.NewTestActivityEnvironment()
			scaffold.Env.RegisterActivity(test.fn)
			_, gotErr := scaffold.Env.ExecuteActivity(test.fn)
			gotErrType, gotErrMessage, unwrappedErr := UnwrapError(gotErr)
			assert.Equal(t, test.wantErrType, gotErrType, "error type")
			assert.Equal(t, test.wantErrMessage, gotErrMessage, "error message")
			switch test.wantErrType {
			case UnwrapErrTypeFailure:
				assert.Equal(t, test.wantErr, unwrappedErr, "unwrapped error")
			case UnwrapErrTypePanic:
				var panicErr PanicError
				if assert.True(t, errors.As(unwrappedErr, &panicErr)) {
					assert.NotEmpty(t, panicErr.StackTrace)
					panicErr.StackTrace = "" // we don't care about our stack trace, just that it has one
					assert.Equal(t, test.wantErr, panicErr, "panic error")
				}
			case UnwrapErrTypeCancellation:
				var cancelErr *temporal.CanceledError
				if assert.True(t, errors.As(unwrappedErr, &cancelErr)) {
					assert.EqualError(t, cancelErr, test.wantErr.Error())
				}
			case UnwrapErrTypeTimeout:
				assert.True(t, errors.Is(unwrappedErr, ErrTimeout))
			}
		})
	}
}

func TestUnwrapError_Workflow(t *testing.T) {
	tests := map[string]struct {
		fn             func(wctx workflow.Context) error
		wantErrType    UnwrapErrType
		wantErrMessage string
		wantErr        error
	}{
		"panic": {
			fn: func(wctx workflow.Context) error {
				var x *url.URL
				x.String() // Intentional nil panic
				return nil
			},
			wantErrType:    UnwrapErrTypePanic,
			wantErrMessage: "runtime error: invalid memory address or nil pointer dereference",
			wantErr:        PanicError{Message: "runtime error: invalid memory address or nil pointer dereference"},
		},
		"cancellation": {
			fn: func(wctx workflow.Context) error {
				wctx, cancel := workflow.WithCancel(wctx)
				cancel()
				return wctx.Err()
			},
			wantErrType:    UnwrapErrTypeCancellation,
			wantErrMessage: ErrSystemCancellation.Error(),
			wantErr:        ErrSystemCancellation,
		},
		"timeout": {
			fn: func(wctx workflow.Context) error {
				workflow.Sleep(wctx, 2*time.Second)
				return nil
			},
			wantErrType:    UnwrapErrTypeTimeout,
			wantErrMessage: ErrTimeout.Error(),
			wantErr:        ErrTimeout,
		},
		"custom-error": {
			fn: func(wctx workflow.Context) error {
				return WrapCustomError(customErrorType{Extra: "extra"})
			},
			wantErrType:    UnwrapErrTypeFailure,
			wantErrMessage: fmt.Sprintf("custom error type: extra"),
			wantErr: customErrorType{
				Extra: "extra",
			},
		},
		"anonymous-error": {
			fn: func(wctx workflow.Context) error {
				return fmt.Errorf("anonymous error message")
			},
			wantErrType:    UnwrapErrTypeFailure,
			wantErrMessage: "anonymous error message",
			wantErr:        errors.New("anonymous error message"),
		},
	}

	RegisterCustomErrorDefault[customErrorType]("customErrorType")

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			scaffold := WorkflowTestScaffold{T: t}
			scaffold.Env = scaffold.NewTestWorkflowEnvironment()
			scaffold.Env.RegisterWorkflow(test.fn)
			scaffold.Env.SetWorkflowRunTimeout(time.Second)
			scaffold.Env.ExecuteWorkflow(test.fn)
			assert.True(t, scaffold.Env.IsWorkflowCompleted())
			gotErr := scaffold.Env.GetWorkflowError()
			gotErrType, gotErrMessage, unwrappedErr := UnwrapError(gotErr)
			assert.Equal(t, test.wantErrType, gotErrType, "error type")
			assert.Equal(t, test.wantErrMessage, gotErrMessage, "error message")
			if test.wantErrType == UnwrapErrTypePanic {
				// we don't really care about the details of the stack trace, we just need to know it has one
				var panicErr PanicError
				errors.As(unwrappedErr, &panicErr)
				assert.NotEmpty(t, panicErr.StackTrace, "panic stack trace should not be empty")
				panicErr.StackTrace = ""
				unwrappedErr = panicErr
			}
			assert.Equal(t, test.wantErr, unwrappedErr, "unwrapped error")
		})
	}
}

// activityCanceledError builds the error a workflow receives when Temporal reports an activity as cancelled,
// the way the failure converter hands it over: an ActivityError wrapping a CanceledError with details
func activityCanceledError(startedEventId int64, details string) error {
	converter := temporal.GetDefaultFailureConverter()
	return converter.FailureToError(&failurepb.Failure{
		Message: "activity error",
		FailureInfo: &failurepb.Failure_ActivityFailureInfo{ActivityFailureInfo: &failurepb.ActivityFailureInfo{
			ScheduledEventId: 78,
			StartedEventId:   startedEventId,
			ActivityType:     &commonpb.ActivityType{Name: "activity/update-tf-run-status"},
			ActivityId:       "78",
			RetryState:       enumspb.RETRY_STATE_NON_RETRYABLE_FAILURE,
		}},
		Cause: converter.ErrorToFailure(temporal.NewCanceledError(details)),
	})
}

// An activity scheduled on an already-cancelled context is cancelled by the server before any worker starts it;
// the server's marker in its details ("ACTIVITY_ID_NOT_STARTED") is not a message and must not surface as one
func TestUnwrapError_ActivityCancelledBeforeStart(t *testing.T) {
	errType, msg, err := UnwrapError(activityCanceledError(0, "ACTIVITY_ID_NOT_STARTED"))
	assert.Equal(t, UnwrapErrTypeCancellation, errType)
	assert.Equal(t, ErrSystemCancellation.Error(), msg)
	assert.ErrorIs(t, err, ErrSystemCancellation)
}

// A started activity that returns a CanceledError chose its message (e.g. "the platform evicted the rollout")
func TestUnwrapError_StartedActivityKeepsItsCancelMessage(t *testing.T) {
	errType, msg, err := UnwrapError(activityCanceledError(79, "the platform evicted the rollout"))
	assert.Equal(t, UnwrapErrTypeCancellation, errType)
	assert.Equal(t, "the platform evicted the rollout", msg)
	var canceledErr *temporal.CanceledError
	assert.ErrorAs(t, err, &canceledErr)
}

// A cancellation that was already deciphered once (its handler returned ErrSystemCancellation) still reads as a cancellation
func TestUnwrapError_RedecipheredSystemCancellation(t *testing.T) {
	errType, msg, err := UnwrapError(ErrSystemCancellation)
	assert.Equal(t, UnwrapErrTypeCancellation, errType)
	assert.Equal(t, ErrSystemCancellation.Error(), msg)
	assert.ErrorIs(t, err, ErrSystemCancellation)
}
