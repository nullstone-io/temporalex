package temporalex

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"time"
)

func DefaultActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 12 * time.Hour,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	}
}

type ActivityInput interface {
	SpanAttributes() []attribute.KeyValue
}

type ActivityRunFunc[TConfig any, TInput any, TResult any] func(ctx context.Context, cfg TConfig, input TInput) (TResult, error)
type HandleResultFunc[TResult any] func(ctx context.Context, result TResult, err error) (TResult, error)

type Activity[TConfig any, TInput ActivityInput, TResult any] struct {
	Name         string
	Options      workflow.ActivityOptions
	Run          ActivityRunFunc[TConfig, TInput, TResult]
	HandleResult HandleResultFunc[TResult]
}

func (a Activity[TConfig, TInput, TResult]) Register(cfg TConfig, registry worker.Registry) {
	opts := activity.RegisterOptions{
		Name:                          a.Name,
		DisableAlreadyRegisteredCheck: true,
		SkipInvalidStructFunctions:    false,
	}
	registry.RegisterActivityWithOptions(a.run(cfg), opts)
}

func (a Activity[TConfig, TInput, TResult]) run(cfg TConfig) func(ctx context.Context, input TInput) (TResult, error) {
	return func(ctx context.Context, input TInput) (TResult, error) {
		var span trace.Span
		aInfo := activity.GetInfo(ctx)
		ctx, span = tracer.Start(ctx, fmt.Sprintf("%s.Run", a.Name),
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(append(
				input.SpanAttributes(),
				attribute.String("temporal.workflow.id", aInfo.WorkflowExecution.ID),
				attribute.String("temporal.workflow.type", aInfo.WorkflowType.Name),
				attribute.String("temporal.activity.id", aInfo.ActivityID),
				attribute.String("temporal.activity.type", aInfo.ActivityType.Name),
			)...),
		)
		defer span.End()

		result, err := a.Run(ctx, cfg, input)
		if a.HandleResult != nil {
			return a.HandleResult(ctx, result, err)
		}
		return result, err
	}
}

func (a Activity[TConfig, TInput, TResult]) Do(wctx workflow.Context, input TInput) (TResult, error) {
	wctx = workflow.WithActivityOptions(wctx, a.Options)
	var result TResult
	err := workflow.ExecuteActivity(wctx, a.Name, input).Get(wctx, &result)
	return result, err
}

func (a Activity[TConfig, TInput, TResult]) DoLocal(wctx workflow.Context, input TInput) (TResult, error) {
	wctx = workflow.WithLocalActivityOptions(wctx, workflow.LocalActivityOptions{
		ScheduleToCloseTimeout: a.Options.ScheduleToCloseTimeout,
		StartToCloseTimeout:    a.Options.StartToCloseTimeout,
		RetryPolicy:            a.Options.RetryPolicy,
	})
	var result TResult
	err := workflow.ExecuteLocalActivity(wctx, a.Name, input).Get(wctx, &result)
	return result, err
}
