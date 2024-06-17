package temporalex

import (
	"context"
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

type ActivityRunFunc[TConfig any, TInput any, TResult any] func(ctx context.Context, cfg TConfig, input TInput) (TResult, error)
type PostActivityFunc[TResult any] func(ctx context.Context, result TResult, err error) (TResult, error)
type HandleActivityFunc[TResult any] func(wctx workflow.Context, result TResult, err error) (TResult, error)

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
	HandleResult HandleActivityFunc[TResult]
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
			return a.PostRun(ctx, result, err)
		}
		return result, err
	}
}

func (a Activity[TConfig, TInput, TResult]) Do(wctx workflow.Context, input TInput) (TResult, error) {
	wctx = workflow.WithActivityOptions(wctx, a.Options)
	var result TResult
	err := workflow.ExecuteActivity(wctx, a.Name, input).Get(wctx, &result)
	if a.HandleResult != nil {
		return a.HandleResult(wctx, result, err)
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
		return a.HandleResult(wctx, result, err)
	}
	return result, err
}
