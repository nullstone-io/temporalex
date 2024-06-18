package temporalex

import (
	"errors"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type UnwrapErrType string

const (
	UnwrapErrTypeNone         UnwrapErrType = ""
	UnwrapErrTypeFailure      UnwrapErrType = "failure"
	UnwrapErrTypePanic        UnwrapErrType = "panic"
	UnwrapErrTypeCancellation UnwrapErrType = "cancellation"
	UnwrapErrTypeTimeout      UnwrapErrType = "timeout"
)

var (
	ErrSystemCancellation = errors.New("system cancellation")
	ErrTimeout            = errors.New("timed out")
)

type PanicError struct {
	Message    string `json:"message"`
	StackTrace string `json:"stackTrace"`
}

func (e PanicError) Error() string { return e.Message }

var _ ErrorWrapper = CancelledError{}

type CancelledError struct {
	Message string `json:"message"`
}

func (e CancelledError) WrapError() error {
	return temporal.NewCanceledError(e.Message)
}

func (e CancelledError) Error() string {
	return e.Message
}

// UnwrapError takes an error from a workflow or activity and unwraps the Temporal skeleton to leave the base error
// This utilizes UnwrapCustomError to extract the strongly-typed error across workflow/activity boundaries
func UnwrapError(inputErr error) (UnwrapErrType, error) {
	var panicErr *temporal.PanicError
	if errors.As(inputErr, &panicErr) {
		return UnwrapErrTypePanic, PanicError{
			Message:    panicErr.Error(),
			StackTrace: panicErr.StackTrace(),
		}
	}

	// These can error when a workflow's context has `Done()`
	if errors.Is(inputErr, workflow.ErrCanceled) {
		return UnwrapErrTypeCancellation, ErrSystemCancellation
	} else if errors.Is(inputErr, workflow.ErrDeadlineExceeded) {
		return UnwrapErrTypeTimeout, ErrTimeout
	}

	var canceledErr *temporal.CanceledError
	if errors.As(inputErr, &canceledErr) {
		var finalErr CancelledError
		canceledErr.Details(&finalErr.Message)
		return UnwrapErrTypeCancellation, finalErr
	}

	var appErr *temporal.ApplicationError
	if errors.As(inputErr, &appErr) {
		return UnwrapErrTypeFailure, UnwrapCustomError(appErr)
	}

	var wflowErr *temporal.WorkflowExecutionError
	if errors.As(inputErr, &wflowErr) {
		return UnwrapErrTypeFailure, wflowErr.Unwrap()
	}
	var actErr *temporal.ActivityError
	if errors.As(inputErr, &actErr) {
		return UnwrapErrTypeFailure, actErr.Unwrap()
	}

	return UnwrapErrTypeFailure, inputErr
}
