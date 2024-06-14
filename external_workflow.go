package temporalex

import (
	"context"
	"fmt"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ExternalWorkflow[TInput WorkflowInput, TResult any] struct {
	Name              string
	TaskQueue         string
	ParentClosePolicy enums.ParentClosePolicy
	// HandleResult executes after the child workflow completes
	// This function executes in the parent workflow that called the executing child workflow
	HandleResult OnResolvedFunc[TResult]
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
	if w.HandleResult != nil {
		return w.HandleResult(wctx, result, err)
	}
	return result, err
}

func (w ExternalWorkflow[TInput, TResult]) DoChildAsync(wctx workflow.Context, input TInput) *TypedFuture[TResult] {
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
	return NewFuture[TResult](workflow.ExecuteChildWorkflow(wctx, w.Name, input), w.HandleResult)
}

func (w ExternalWorkflow[TInput, TResult]) Do(ctx context.Context, temporalClient client.Client, input TInput) (TResult, error) {
	var result TResult
	if run, err := w.DoAsync(ctx, temporalClient, input); err != nil {
		return result, err
	} else if err = run.Get(ctx, &result); err != nil {
		return result, fmt.Errorf("error in external workflow: %w", err)
	}
	return result, nil
}

func (w ExternalWorkflow[TInput, TResult]) DoAsync(ctx context.Context, temporalClient client.Client, input TInput) (client.WorkflowRun, error) {
	opts := client.StartWorkflowOptions{
		ID:                    input.GetTemporalWorkflowId(w.Name),
		TaskQueue:             w.TaskQueue,
		RetryPolicy:           &temporal.RetryPolicy{MaximumAttempts: 1},
		TypedSearchAttributes: temporal.NewSearchAttributes(input.SearchAttributes()...),
	}
	return temporalClient.ExecuteWorkflow(ctx, opts, w.Name, input)
}
