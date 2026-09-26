package temporalex

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Metric names emitted by Workflow.run and Activity.run
const (
	MetricWorkflowCompletions = "nullstone.workflow.completions"
	MetricWorkflowFailures    = "nullstone.workflow.failures"
	MetricActivityFailures    = "nullstone.activity.failures"
)

// Metric attribute keys. Deliberately low-cardinality: org/stack/env stay on spans only.
const (
	MetricAttrStatus       = "status"
	MetricAttrClass        = "class"
	MetricAttrCategory     = "category"
	MetricAttrWorkflowType = "workflow_type"
	MetricAttrActivityType = "activity_type"
	// MetricAttrRoot is true for a workflow with no parent, i.e. one count per customer-visible intent
	MetricAttrRoot = "root"
)

// Completion statuses for MetricWorkflowCompletions
const (
	CompletionSucceeded = "succeeded"
	CompletionFailed    = "failed"
	CompletionCancelled = "cancelled"
	CompletionTimeout   = "timeout"
)

var (
	workflowCompletions metric.Int64Counter = noop.Int64Counter{}
	workflowFailures    metric.Int64Counter = noop.Int64Counter{}
	activityFailures    metric.Int64Counter = noop.Int64Counter{}
)

func init() {
	// The global MeterProvider delegates to whatever provider is registered later (go-telemetry sets it at
	// service start), the same way the package-level tracer does, so instruments can be created at init.
	meter := otel.Meter(instrumentationName)
	workflowCompletions = mustCounter(meter, MetricWorkflowCompletions,
		"Workflow executions that finished, by final status", "{workflow}")
	workflowFailures = mustCounter(meter, MetricWorkflowFailures,
		"Workflow executions that failed, by failure class and category", "{workflow}")
	activityFailures = mustCounter(meter, MetricActivityFailures,
		"Activity executions that failed, by failure class and category", "{activity}")
}

func mustCounter(meter metric.Meter, name, description, unit string) metric.Int64Counter {
	counter, err := meter.Int64Counter(name, metric.WithDescription(description), metric.WithUnit(unit))
	if err != nil || counter == nil {
		return noop.Int64Counter{}
	}
	return counter
}

func completionStatus(class FailureClass) string {
	switch class {
	case FailureClassCancelled:
		return CompletionCancelled
	case FailureClassTimeout:
		return CompletionTimeout
	}
	return CompletionFailed
}

func recordWorkflowMetrics(workflowType string, root bool, info FailureInfo, err error) {
	ctx := context.Background()
	status := CompletionSucceeded
	if err != nil {
		status = completionStatus(info.Class)
	}
	workflowCompletions.Add(ctx, 1, metric.WithAttributes(
		attribute.String(MetricAttrStatus, status),
		attribute.String(MetricAttrWorkflowType, workflowType),
	))
	if err != nil && info.Class.IsFailure() {
		workflowFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String(MetricAttrClass, string(info.Class)),
			attribute.String(MetricAttrCategory, info.Category),
			attribute.String(MetricAttrWorkflowType, workflowType),
			attribute.Bool(MetricAttrRoot, root),
		))
	}
}

func recordActivityMetrics(ctx context.Context, activityType string, info FailureInfo) {
	if !info.Class.IsFailure() {
		return
	}
	activityFailures.Add(ctx, 1, metric.WithAttributes(
		attribute.String(MetricAttrClass, string(info.Class)),
		attribute.String(MetricAttrCategory, info.Category),
		attribute.String(MetricAttrActivityType, activityType),
	))
}
