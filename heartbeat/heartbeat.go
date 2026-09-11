// Package heartbeat records Temporal activity heartbeats around a long-running unit of work.
//
// Temporal only delivers cancellation to an activity that heartbeats: without a heartbeat the
// activity's context is never cancelled and a cancelled workflow cannot stop a running process.
// Wrap any activity body that shells out or polls for a long time in Run, and give the activity
// options from ActivityOptions so the server knows how often to expect a heartbeat.
package heartbeat

import (
	"context"
	"sync"
	"time"

	"github.com/nullstone-io/temporalex"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

// DefaultInterval is how often Run records a heartbeat when no interval is given
const DefaultInterval = 30 * time.Second

// isActivity and recordHeartbeat are indirected so tests can observe every tick;
// the SDK coalesces heartbeats recorded within its throttle window
var (
	isActivity      = activity.IsActivity
	recordHeartbeat = func(ctx context.Context) { activity.RecordHeartbeat(ctx) }
)

// ActivityOptions returns temporalex.DefaultActivityOptions with a HeartbeatTimeout sized for
// activities that heartbeat every interval. The timeout tolerates two missed heartbeats.
//
// A heartbeating activity is one that reacts to cancellation (it learns of the cancel through its
// heartbeats and stops what it is running), so the workflow waits for the activity to actually finish
// instead of treating it as cancelled the instant the cancel is requested. Without this the workflow
// would record a terminal status while the process the activity started is still running.
func ActivityOptions(taskQueue string, interval time.Duration) workflow.ActivityOptions {
	if interval <= 0 {
		interval = DefaultInterval
	}
	opts := temporalex.DefaultActivityOptions(taskQueue)
	opts.HeartbeatTimeout = 3 * interval
	opts.WaitForCancellation = true
	return opts
}

// Run executes fn while recording an activity heartbeat every interval until fn returns.
// A heartbeat is recorded immediately before fn starts. When ctx is not an activity context
// (e.g. unit tests calling the activity body directly), no heartbeats are recorded and fn simply runs.
func Run[T any](ctx context.Context, interval time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	if !isActivity(ctx) {
		return fn(ctx)
	}
	if interval <= 0 {
		interval = DefaultInterval
	}

	recordHeartbeat(ctx)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				recordHeartbeat(ctx)
			}
		}
	}()
	defer func() {
		close(stop)
		wg.Wait()
	}()

	return fn(ctx)
}
