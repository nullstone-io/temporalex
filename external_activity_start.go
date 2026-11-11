package temporalex

import (
	"context"

	"go.temporal.io/sdk/client"
)

// Start schedules the activity as a standalone activity execution: directly on its task queue, with no
// workflow around it. It returns once the server accepts the start; use the handle to wait for the result.
//
// opts.ID is required. TaskQueue, timeouts and RetryPolicy left empty in opts are filled from the activity's
// own TaskQueue and Options. The Temporal Service must support standalone activities.
func (a ExternalActivity[TInput, TResult]) Start(ctx context.Context, temporalClient client.Client, opts client.StartActivityOptions, input TInput) (client.ActivityHandle, error) {
	return temporalClient.ExecuteActivity(ctx, a.startOptions(opts), a.Name, input)
}

func (a ExternalActivity[TInput, TResult]) startOptions(opts client.StartActivityOptions) client.StartActivityOptions {
	if opts.TaskQueue == "" {
		opts.TaskQueue = a.TaskQueue
	}
	if opts.TaskQueue == "" {
		opts.TaskQueue = a.Options.TaskQueue
	}
	if opts.ScheduleToCloseTimeout == 0 {
		opts.ScheduleToCloseTimeout = a.Options.ScheduleToCloseTimeout
	}
	if opts.ScheduleToStartTimeout == 0 {
		opts.ScheduleToStartTimeout = a.Options.ScheduleToStartTimeout
	}
	if opts.StartToCloseTimeout == 0 {
		opts.StartToCloseTimeout = a.Options.StartToCloseTimeout
	}
	if opts.HeartbeatTimeout == 0 {
		opts.HeartbeatTimeout = a.Options.HeartbeatTimeout
	}
	if opts.RetryPolicy == nil {
		opts.RetryPolicy = a.Options.RetryPolicy
	}
	return opts
}
