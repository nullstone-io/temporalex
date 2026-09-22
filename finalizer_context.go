package temporalex

import "go.temporal.io/sdk/workflow"

// FinalizerContext returns wctx, or a disconnected copy of it when wctx has already been cancelled.
//
// PostRun and HandleResult functions are finalizers: they record terminal statuses, release locks, and
// signal other workflows. Temporal cancels an activity or child workflow started on a cancelled context
// before it ever runs, so a finalizer executing on the raw context after a cancellation would silently
// skip its cleanup. Running it on a disconnected context lets the cleanup complete.
// See https://docs.temporal.io/develop/go/cancellation#handle-cancellation-in-workflow
func FinalizerContext(wctx workflow.Context) workflow.Context {
	if wctx.Err() == nil {
		return wctx
	}
	dctx, _ := workflow.NewDisconnectedContext(wctx)
	return dctx
}
