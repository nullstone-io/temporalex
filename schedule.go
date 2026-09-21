package temporalex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
)

// scheduleReconcileTimeout bounds the Temporal calls made while registering a Schedule.
// Register has no context of its own; it runs once at worker start.
const scheduleReconcileTimeout = 30 * time.Second

var _ Registrar[stub] = Schedule[stub, WorkflowInput, any]{}

// Schedule pairs a Workflow with a Temporal Schedule that starts it on a recurring Spec.
//
// Registering a Schedule registers its Workflow with the worker and reconciles the schedule in
// Temporal: the schedule is created when missing and updated otherwise, so its spec, action, and
// policies always match the code that was deployed. State an operator sets through Temporal
// (paused, remaining actions, note) is preserved across updates.
//
// A Schedule is the Workflow's registrar: list the Schedule in the worker's registration set
// instead of the Workflow itself.
type Schedule[TConfig any, TInput WorkflowInput, TResult any] struct {
	// ID is the schedule id in Temporal. It must be unique within the namespace.
	ID       string
	Workflow Workflow[TConfig, TInput, TResult]
	// Input is passed to every run the schedule starts and names the run's workflow id.
	// Temporal appends the scheduled time to that id so successive runs do not collide.
	Input  TInput
	Spec   client.ScheduleSpec
	Policy client.SchedulePolicies
	// Client resolves the Temporal client from the worker's config. When nil, Register only
	// registers the workflow and does not touch Temporal; this is the test configuration.
	Client func(cfg TConfig) client.Client
}

// Register registers the workflow and reconciles the schedule in Temporal. It panics when the
// schedule cannot be reconciled: a worker that cannot reach Temporal cannot run anyway, and a
// schedule drifting from the deployed code should stop the deploy rather than pass silently.
func (s Schedule[TConfig, TInput, TResult]) Register(cfg TConfig, registry worker.Registry) {
	s.Workflow.Register(cfg, registry)
	if s.Client == nil {
		return
	}
	temporalClient := s.Client(cfg)
	if temporalClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), scheduleReconcileTimeout)
	defer cancel()
	if err := s.Ensure(ctx, temporalClient); err != nil {
		panic(err)
	}
}

// Ensure creates the schedule in Temporal, or updates an existing one so that its spec, action,
// and policies match this definition. Operator state (paused, remaining actions, note) is kept.
func (s Schedule[TConfig, TInput, TResult]) Ensure(ctx context.Context, temporalClient client.Client) error {
	scheduleClient := temporalClient.ScheduleClient()
	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID:             s.ID,
		Spec:           s.Spec,
		Action:         s.action(),
		Overlap:        s.Policy.Overlap,
		CatchupWindow:  s.Policy.CatchupWindow,
		PauseOnFailure: s.Policy.PauseOnFailure,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("error creating schedule %q: %w", s.ID, err)
	}

	err = scheduleClient.GetHandle(ctx, s.ID).Update(ctx, client.ScheduleUpdateOptions{DoUpdate: s.update})
	if err != nil {
		return fmt.Errorf("error updating schedule %q: %w", s.ID, err)
	}
	return nil
}

// update is the DoUpdate callback: it replaces what the code owns and leaves State untouched,
// since pausing a schedule is an operator decision that a deploy must not undo.
func (s Schedule[TConfig, TInput, TResult]) update(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
	spec, policy := s.Spec, s.Policy
	updated := input.Description.Schedule
	updated.Action = s.action()
	updated.Spec = &spec
	updated.Policy = &policy
	return &client.ScheduleUpdate{Schedule: &updated}, nil
}

func (s Schedule[TConfig, TInput, TResult]) action() *client.ScheduleWorkflowAction {
	return &client.ScheduleWorkflowAction{
		ID:                    s.Input.GetTemporalWorkflowId(s.Workflow.Name),
		Workflow:              s.Workflow.Name,
		Args:                  []any{s.Input},
		TaskQueue:             s.Workflow.TaskQueue,
		RetryPolicy:           &temporal.RetryPolicy{MaximumAttempts: 1},
		TypedSearchAttributes: temporal.NewSearchAttributes(s.Input.SearchAttributes()...),
	}
}
