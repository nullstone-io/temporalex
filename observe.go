package temporalex

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/workflow"
)

// WorkflowOutcome describes how a Workflow execution finished. It is reported once per execution,
// after PostRun, and never while the workflow is replaying.
type WorkflowOutcome struct {
	WorkflowType string
	// Root is true when the workflow has no parent execution
	Root bool
	// Err is the error PostRun returned (nil on success), before it is wrapped for the Temporal boundary
	Err error
	// Span is the `<name>.Run` span temporalex opened for the execution
	Span trace.Span
	// ParentSpan is the `RunWorkflow:<type>` span the Temporal tracing interceptor owns (the one that records
	// the workflow's final error); a no-op span when no interceptor is registered, so callers need no nil check
	ParentSpan trace.Span
}

// ActivityOutcome describes how an Activity execution finished. It is reported once per attempt, after PostRun.
type ActivityOutcome struct {
	ActivityType string
	// Err is the error PostRun returned (nil on success), before it is wrapped for the Temporal boundary
	Err error
}

// A WorkflowObserver is told the outcome of every Workflow execution (e.g. to classify and count failures).
// wctx is a FinalizerContext-safe workflow context; observers must not block or perform workflow commands.
type WorkflowObserver func(wctx workflow.Context, outcome WorkflowOutcome)

// An ActivityObserver is told the outcome of every Activity execution. ctx is the activity's context,
// whose span is the interceptor's `RunActivity:<name>` span.
type ActivityObserver func(ctx context.Context, outcome ActivityOutcome)

var (
	workflowObservers []WorkflowObserver
	activityObservers []ActivityObserver
)

// ObserveWorkflows registers fn to be told every Workflow outcome
func ObserveWorkflows(fn WorkflowObserver) {
	workflowObservers = append(workflowObservers, fn)
}

// ObserveActivities registers fn to be told every Activity outcome
func ObserveActivities(fn ActivityObserver) {
	activityObservers = append(activityObservers, fn)
}

func notifyWorkflowObservers(wctx workflow.Context, span trace.Span, err error) {
	// Workflow code re-executes during replay; the outcome only happened once
	if len(workflowObservers) == 0 || workflow.IsReplaying(wctx) {
		return
	}
	wInfo := workflow.GetInfo(wctx)
	outcome := WorkflowOutcome{
		WorkflowType: wInfo.WorkflowType.Name,
		Root:         wInfo.ParentWorkflowExecution == nil,
		Err:          err,
		Span:         span,
		ParentSpan:   WorkflowSpan(wctx),
	}
	for _, observe := range workflowObservers {
		observe(wctx, outcome)
	}
}

func notifyActivityObservers(ctx context.Context, activityType string, err error) {
	outcome := ActivityOutcome{ActivityType: activityType, Err: err}
	for _, observe := range activityObservers {
		observe(ctx, outcome)
	}
}
