package temporalex

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func DefaultActivityOptions(taskQueue string) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		TaskQueue:           taskQueue,
		StartToCloseTimeout: 12 * time.Hour,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	}
}

type ActivityRunFunc[TConfig any, TInput any, TResult any] func(ctx context.Context, cfg TConfig, input TInput) (TResult, error)
type PostActivityFunc[TResult any] func(ctx context.Context, result TResult, err error) (TResult, error)
type HandleActivityFunc[TInput any, TResult any] func(wctx workflow.Context, input TInput, result TResult, err error) (TResult, error)

var _ Registrar[stub] = Activity[stub, any, any]{}

type Activity[TConfig any, TInput any, TResult any] struct {
	Name    string
	Options workflow.ActivityOptions
	Run     ActivityRunFunc[TConfig, TInput, TResult]
	// PostRun executes before completing the activity
	// This function executes inside the registered function of the activity
	// This function is useful for finalizing execution of an activity
	PostRun PostActivityFunc[TResult]
	// HandleResult executes after the activity completes
	// This function executes in the workflow that called the activity
	HandleResult HandleActivityFunc[TInput, TResult]
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
		result, err := a.Run(ctx, cfg, input)
		if a.PostRun != nil {
			result, err = a.PostRun(ctx, result, err)
		}
		if err != nil {
			// Registered error types (including WithFailure classifications) only survive the Temporal boundary
			// as application errors; wrapping here is idempotent for PostRuns that already did it
			err = WrapCustomError(err)
			a.recordFailure(ctx, err)
		}
		return result, err
	}
}

// recordFailure classifies the activity's final error onto the tracing interceptor's `RunActivity:<name>` span
// (which is the span in ctx, and the one that records the error) and counts it by class/category.
// This is the single place an activity failure is classified; HandleResult sees the same error in the workflow.
func (a Activity[TConfig, TInput, TResult]) recordFailure(ctx context.Context, err error) {
	info, unwrapped := classify(err)
	trace.SpanFromContext(ctx).SetAttributes(info.Attributes(errorTypeName(unwrapped))...)
	recordActivityMetrics(ctx, a.Name, info)
}

func (a Activity[TConfig, TInput, TResult]) Do(wctx workflow.Context, input TInput) (TResult, error) {
	wctx = workflow.WithActivityOptions(wctx, a.Options)
	var result TResult
	err := workflow.ExecuteActivity(wctx, a.Name, input).Get(wctx, &result)
	if a.HandleResult != nil {
		return a.HandleResult(FinalizerContext(wctx), input, result, err)
	}
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
	if a.HandleResult != nil {
		return a.HandleResult(FinalizerContext(wctx), input, result, err)
	}
	return result, err
}
