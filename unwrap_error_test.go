package temporalex

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
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
		fn          func(ctx context.Context) error
		wantErrType UnwrapErrType
		wantErr     error
	}{
		"panic": {
			fn: func(ctx context.Context) error {
				var x *url.URL
				x.String() // Intentional nil panic
				return nil
			},
			wantErrType: UnwrapErrTypePanic,
			wantErr:     PanicError{Message: "runtime error: invalid memory address or nil pointer dereference"},
		},
		"cancellation": {
			fn: func(ctx context.Context) error {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				return temporal.NewCanceledError(ctx.Err().Error())
			},
			wantErrType: UnwrapErrTypeCancellation,
			wantErr:     CancelledError{Message: context.Canceled.Error()},
		},
		"timeout": {
			fn: func(ctx context.Context) error {
				ctx, cancel := context.WithDeadline(ctx, time.Now())
				defer cancel()
				time.Sleep(time.Nanosecond)
				return temporal.NewCanceledError(ctx.Err().Error())
			},
			wantErrType: UnwrapErrTypeCancellation,
			wantErr:     CancelledError{Message: context.DeadlineExceeded.Error()},
		},
		"custom-error": {
			fn: func(ctx context.Context) error {
				return WrapCustomError(customErrorType{Extra: "extra"})
			},
			wantErrType: UnwrapErrTypeFailure,
			wantErr: customErrorType{
				Extra: "extra",
			},
		},
		"anonymous-error": {
			fn: func(ctx context.Context) error {
				return fmt.Errorf("anonymous error message")
			},
			wantErrType: UnwrapErrTypeFailure,
			wantErr:     errors.New("anonymous error message"),
		},
	}

	RegisterCustomErrorDefault[customErrorType]("customErrorType")

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			scaffold := ActivityTestScaffold{T: t}
			scaffold.Env = scaffold.NewTestActivityEnvironment()
			scaffold.Env.RegisterActivity(test.fn)
			_, gotErr := scaffold.Env.ExecuteActivity(test.fn)
			gotErrType, unwrappedErr := UnwrapError(gotErr)
			assert.Equal(t, test.wantErrType, gotErrType, "error type")
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

func TestUnwrapError_Workflow(t *testing.T) {
	tests := map[string]struct {
		fn          func(wctx workflow.Context) error
		wantErrType UnwrapErrType
		wantErr     error
	}{
		"panic": {
			fn: func(wctx workflow.Context) error {
				var x *url.URL
				x.String() // Intentional nil panic
				return nil
			},
			wantErrType: UnwrapErrTypePanic,
			wantErr:     PanicError{Message: "runtime error: invalid memory address or nil pointer dereference"},
		},
		"cancellation": {
			fn: func(wctx workflow.Context) error {
				wctx, cancel := workflow.WithCancel(wctx)
				cancel()
				return wctx.Err()
			},
			wantErrType: UnwrapErrTypeCancellation,
			wantErr:     ErrSystemCancellation,
		},
		"timeout": {
			fn: func(wctx workflow.Context) error {
				workflow.Sleep(wctx, 2*time.Second)
				return nil
			},
			wantErrType: UnwrapErrTypeTimeout,
			wantErr:     ErrTimeout,
		},
		"custom-error": {
			fn: func(wctx workflow.Context) error {
				return WrapCustomError(customErrorType{Extra: "extra"})
			},
			wantErrType: UnwrapErrTypeFailure,
			wantErr: customErrorType{
				Extra: "extra",
			},
		},
		"anonymous-error": {
			fn: func(wctx workflow.Context) error {
				return fmt.Errorf("anonymous error message")
			},
			wantErrType: UnwrapErrTypeFailure,
			wantErr:     errors.New("anonymous error message"),
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
			gotErrType, unwrappedErr := UnwrapError(gotErr)
			assert.Equal(t, test.wantErrType, gotErrType, "error type")
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
