package temporalex

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type RunFunc[TConfig any, TInput any, TResult any] func(wctx workflow.Context, ctx context.Context, cfg TConfig, input TInput) (TResult, error)
type PostRunFunc[TInput any, TResult any] func(wctx workflow.Context, input TInput, result TResult, err error) (TResult, error)
type HandleWorkflowFunc[TInput any, TResult any] func(wctx workflow.Context, ctx context.Context, input TInput, result TResult, err error) (TResult, error)

var _ Registrar[stub] = Workflow[stub, WorkflowInput, any]{}

type Workflow[TConfig any, TInput WorkflowInput, TResult any] struct {
	Name              string
	TaskQueue         string
	ParentClosePolicy enums.ParentClosePolicy
	Activities        []Registrar[TConfig]
	Run               RunFunc[TConfig, TInput, TResult]
	// PostRun executes before completing the child workflow
	// This function executes inside the registered function of the child workflow
	// This function is useful for finalizing execution of a child workflow
	PostRun PostRunFunc[TInput, TResult]
	// HandleResult executes after the child workflow completes
	// This function executes in the parent workflow that called the executing child workflow
	HandleResult HandleWorkflowFunc[TInput, TResult]
}

func (w Workflow[TConfig, TInput, TResult]) Register(cfg TConfig, registry worker.Registry) {
	registry.RegisterWorkflowWithOptions(w.run(cfg), workflow.RegisterOptions{
		Name:                          w.Name,
		DisableAlreadyRegisteredCheck: true,
	})
	for _, activity := range w.Activities {
		activity.Register(cfg, registry)
	}
}

func (w Workflow[TConfig, TInput, TResult]) run(cfg TConfig) func(wctx workflow.Context, input TInput) (TResult, error) {
	return func(wctx workflow.Context, input TInput) (TResult, error) {
		wInfo := workflow.GetInfo(wctx)
		attrs := append(
			input.SpanAttributes(),
			attribute.String("temporal.workflow.id", wInfo.WorkflowExecution.ID),
			attribute.String("temporal.workflow.type", wInfo.WorkflowType.Name),
		)

		// The Temporal tracing interceptor owns the `RunWorkflow:<type>` span, and it is the only
		// span that records the workflow's final error. Mirror our attributes onto it so failures
		// are filterable by the input's own dimensions, and use it as our parent so both spans land
		// in the same trace. Without this, our span is a root in a trace of its own and the span
		// carrying the error has no attributes to filter or group on.
		ctx := context.Background()
		if parent := WorkflowSpan(wctx); parent.SpanContext().IsValid() {
			parent.SetAttributes(attrs...)
			ctx = trace.ContextWithSpan(ctx, parent)
		}

		var span trace.Span
		ctx, span = tracer.Start(ctx, fmt.Sprintf("%s.Run", w.Name),
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(attrs...),
		)
		defer span.End()

		result, err := w.Run(wctx, ctx, cfg, input)
		if w.PostRun != nil {
			// PostRun deciphers the error, so record what it returns rather than the raw error
			result, err = w.PostRun(wctx, input, result, err)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return result, err
	}
}

func (w Workflow[TConfig, TInput, TResult]) DoChild(wctx workflow.Context, ctx context.Context, input TInput) (TResult, error) {
	pcp := w.ParentClosePolicy
	if pcp == 0 {
		pcp = enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL
	}
	wctx = workflow.WithChildOptions(wctx, workflow.ChildWorkflowOptions{
		WorkflowID:            input.GetTemporalWorkflowId(w.Name),
		ParentClosePolicy:     pcp,
		TypedSearchAttributes: temporal.NewSearchAttributes(input.SearchAttributes()...),
	})
	var result TResult
	err := workflow.ExecuteChildWorkflow(wctx, w.Name, input).Get(wctx, &result)
	if w.HandleResult != nil {
		return w.HandleResult(wctx, ctx, input, result, err)
	}
	return result, err
}

func (w Workflow[TConfig, TInput, TResult]) DoChildAsync(wctx workflow.Context, ctx context.Context, input TInput) *TypedFuture[TResult] {
	pcp := w.ParentClosePolicy
	if pcp == 0 {
		pcp = enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL
	}
	wctx = workflow.WithChildOptions(wctx, workflow.ChildWorkflowOptions{
		WorkflowID:            input.GetTemporalWorkflowId(w.Name),
		ParentClosePolicy:     pcp,
		TypedSearchAttributes: temporal.NewSearchAttributes(input.SearchAttributes()...),
	})
	return NewFuture[TResult](workflow.ExecuteChildWorkflow(wctx, w.Name, input), func(wctx workflow.Context, result TResult, err error) (TResult, error) {
		if w.HandleResult != nil {
			return w.HandleResult(wctx, ctx, input, result, err)
		}
		return result, err
	})
}

func (w Workflow[TConfig, TInput, TResult]) Do(ctx context.Context, temporalClient client.Client, input TInput) (TResult, error) {
	var result TResult
	if run, err := w.DoAsync(ctx, temporalClient, input); err != nil {
		return result, err
	} else if err = run.Get(ctx, &result); err != nil {
		return result, fmt.Errorf("error in workflow: %w", err)
	}
	return result, nil
}

func (w Workflow[TConfig, TInput, TResult]) DoAsync(ctx context.Context, temporalClient client.Client, input TInput) (client.WorkflowRun, error) {
	opts := client.StartWorkflowOptions{
		ID:                    input.GetTemporalWorkflowId(w.Name),
		TaskQueue:             w.TaskQueue,
		RetryPolicy:           &temporal.RetryPolicy{MaximumAttempts: 1},
		TypedSearchAttributes: temporal.NewSearchAttributes(input.SearchAttributes()...),
	}
	return temporalClient.ExecuteWorkflow(ctx, opts, w.Name, input)
}
