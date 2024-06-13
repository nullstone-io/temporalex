package temporalex

import "go.temporal.io/sdk/workflow"

type ExternalActivity[TInput any, TResult any] struct {
	Name      string
	TaskQueue string
	Options   workflow.ActivityOptions
	// HandleResult executes after the activity completes
	// This function executes in the workflow that called the activity
	HandleResult HandleActivityFunc[TResult]
}

func (a ExternalActivity[TInput, TResult]) Do(wctx workflow.Context, input TInput) (TResult, error) {
	wctx = workflow.WithActivityOptions(wctx, a.Options)
	var result TResult
	err := workflow.ExecuteActivity(wctx, a.Name, input).Get(wctx, &result)
	if a.HandleResult != nil {
		return a.HandleResult(wctx, result, err)
	}
	return result, err
}
