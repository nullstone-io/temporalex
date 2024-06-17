package temporalex

import (
	"go.opentelemetry.io/otel/attribute"
	"go.temporal.io/sdk/temporal"
)

type WorkflowInput interface {
	GetTemporalWorkflowId(name string) string
	SearchAttributes() []temporal.SearchAttributeUpdate
	SpanAttributes() []attribute.KeyValue
}
