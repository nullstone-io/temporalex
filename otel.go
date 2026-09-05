package temporalex

import (
	"go.opentelemetry.io/otel/trace"
	oteltemporal "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/workflow"
)

// WorkflowSpan returns the OpenTelemetry span that the Temporal tracing interceptor created for the
// currently-executing workflow (the `RunWorkflow:<type>` span, which is also the span the
// interceptor records the workflow's final error on).
//
// Workflow code has no context.Context, so reaching a recording span from inside a workflow means
// going through workflow.Context. Do NOT use trace.SpanFromContext(context.TODO()) instead: that
// returns a non-recording no-op span, so everything recorded on it is silently discarded.
//
// When no interceptor span is present (e.g. tests without the tracing interceptor registered) this
// returns a no-op span, so callers never need a nil check.
func WorkflowSpan(wctx workflow.Context) trace.Span {
	span, _ := oteltemporal.SpanFromWorkflowContext(wctx)
	return span
}
