package tracing

import (
	"context"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

const (
	defaultPropagationKey = "temporal-propagation"
)

// contextKey is an unexported type used as key for items stored in the
// Context object
type contextKey struct{}

// PropagateContextKey is the key used to store the value in the Context object
var PropagateContextKey = contextKey{}

// Values is a struct holding values
type Values struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func NewContextPropagator(propagationKey string) workflow.ContextPropagator {
	if propagationKey == "" {
		propagationKey = defaultPropagationKey
	}
	return &ContextPropagator{
		PropagationKey: propagationKey,
	}
}

type ContextPropagator struct {
	// PropagationKey is the key used by the propagator to pass values through the Temporal server headers
	PropagationKey string
}

// Inject injects values from context into headers for propagation
func (s *ContextPropagator) Inject(ctx context.Context, writer workflow.HeaderWriter) error {
	value := ctx.Value(PropagateContextKey)
	payload, err := converter.GetDefaultDataConverter().ToPayload(value)
	if err != nil {
		return err
	}
	writer.Set(s.PropagationKey, payload)
	return nil
}

// InjectFromWorkflow injects values from context into headers for propagation
func (s *ContextPropagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	value := ctx.Value(PropagateContextKey)
	payload, err := converter.GetDefaultDataConverter().ToPayload(value)
	if err != nil {
		return err
	}
	writer.Set(s.PropagationKey, payload)
	return nil
}

// Extract extracts values from headers and puts them into context
func (s *ContextPropagator) Extract(ctx context.Context, reader workflow.HeaderReader) (context.Context, error) {
	if value, ok := reader.Get(s.PropagationKey); ok {
		var values Values
		if err := converter.GetDefaultDataConverter().FromPayload(value, &values); err != nil {
			return ctx, nil
		}
		ctx = context.WithValue(ctx, PropagateContextKey, values)
	}

	return ctx, nil
}

// ExtractToWorkflow extracts values from headers and puts them into context
func (s *ContextPropagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	if value, ok := reader.Get(s.PropagationKey); ok {
		var values Values
		if err := converter.GetDefaultDataConverter().FromPayload(value, &values); err != nil {
			return ctx, nil
		}
		ctx = workflow.WithValue(ctx, PropagateContextKey, values)
	}

	return ctx, nil
}
