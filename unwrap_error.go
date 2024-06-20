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

// UnwrapError takes an error from a workflow or activity and unwraps the Temporal skeleton to leave the base error
// This utilizes UnwrapCustomError to extract the strongly-typed error across workflow/activity boundaries
func UnwrapError(inputErr error) (UnwrapErrType, string, error) {
	curErr := inputErr

	var appErr *temporal.ApplicationError
	if errors.As(curErr, &appErr) {
		curErr = UnwrapCustomError(appErr)
	}

	var panicErr *temporal.PanicError
	if errors.As(curErr, &panicErr) {
		return UnwrapErrTypePanic, panicErr.Error(), PanicError{
			Message:    panicErr.Error(),
			StackTrace: panicErr.StackTrace(),
		}
	}

	// These can error when a workflow's context has `Done()`
	if errors.Is(curErr, workflow.ErrCanceled) {
		return UnwrapErrTypeCancellation, ErrSystemCancellation.Error(), ErrSystemCancellation
	} else if errors.Is(curErr, workflow.ErrDeadlineExceeded) {
		return UnwrapErrTypeTimeout, ErrTimeout.Error(), ErrTimeout
	}

	var canceledErr *temporal.CanceledError
	if errors.As(curErr, &canceledErr) {
		var msg string
		canceledErr.Details(&msg)
		return UnwrapErrTypeCancellation, msg, curErr
	}

	var wflowErr *temporal.WorkflowExecutionError
	var actErr *temporal.ActivityError
	if errors.As(curErr, &wflowErr) {
		curErr = wflowErr.Unwrap()
	} else if errors.As(curErr, &actErr) {
		curErr = actErr.Unwrap()
	}
	return UnwrapErrTypeFailure, curErr.Error(), curErr
}
