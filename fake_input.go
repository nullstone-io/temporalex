package temporalex

import (
	"go.opentelemetry.io/otel/attribute"
	"go.temporal.io/sdk/temporal"
)

var _ WorkflowInput = FakeInput{}

type FakeInput struct {
	TemporalWorkflowId string
}

func (f FakeInput) GetTemporalWorkflowId(name string) string {
	return f.TemporalWorkflowId
}

func (f FakeInput) SearchAttributes() []temporal.SearchAttributeUpdate {
	return []temporal.SearchAttributeUpdate{}
}

func (f FakeInput) SpanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Bool("testing", true),
	}
}
