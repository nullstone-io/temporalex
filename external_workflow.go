package temporalex

import (
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ExternalWorkflow[TInput WorkflowInput, TResult any] struct {
	Name              string
	TaskQueue         string
	ParentClosePolicy enums.ParentClosePolicy
	PostRun           PostFunc[TResult]
}

func (w ExternalWorkflow[TInput, TResult]) DoChild(wctx workflow.Context, input TInput) (TResult, error) {
	pcp := w.ParentClosePolicy
	if pcp == 0 {
		pcp = enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL
	}
	wctx = workflow.WithChildOptions(wctx, workflow.ChildWorkflowOptions{
		TaskQueue:             w.TaskQueue,
		WorkflowID:            input.GetTemporalWorkflowId(w.Name),
		ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		TypedSearchAttributes: temporal.NewSearchAttributes(input.SearchAttributes()...),
	})
	var result TResult
	err := workflow.ExecuteChildWorkflow(wctx, w.Name, input).Get(wctx, &result)
	if w.PostRun != nil {
		return w.PostRun(wctx, result, err)
	}
	return result, err
}

func (w ExternalWorkflow[TInput, TResult]) DoChildAsync(wctx workflow.Context, input TInput) TypedFuture[TResult] {
	pcp := w.ParentClosePolicy
	if pcp == 0 {
		pcp = enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL
	}
	wctx = workflow.WithChildOptions(wctx, workflow.ChildWorkflowOptions{
		TaskQueue:             w.TaskQueue,
		WorkflowID:            input.GetTemporalWorkflowId(w.Name),
		ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		TypedSearchAttributes: temporal.NewSearchAttributes(input.SearchAttributes()...),
	})
	return WrapFuture[TResult](wctx, workflow.ExecuteChildWorkflow(wctx, w.Name, input), w.PostRun)
}
