package temporalex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// The fakes embed the client interfaces so only the schedule calls need implementing;
// anything else panics with a nil dereference, which is what a test should do.
type fakeTemporalClient struct {
	client.Client
	schedules *fakeScheduleClient
}

func (f fakeTemporalClient) ScheduleClient() client.ScheduleClient { return f.schedules }

type fakeScheduleClient struct {
	client.ScheduleClient
	createErr   error
	created     *client.ScheduleOptions
	handle      *fakeScheduleHandle
	getHandleId string
}

func (f *fakeScheduleClient) Create(ctx context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error) {
	f.created = &options
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.handle, nil
}

func (f *fakeScheduleClient) GetHandle(ctx context.Context, scheduleID string) client.ScheduleHandle {
	f.getHandleId = scheduleID
	return f.handle
}

type fakeScheduleHandle struct {
	client.ScheduleHandle
	update    *client.ScheduleUpdateOptions
	updateErr error
}

func (f *fakeScheduleHandle) Update(ctx context.Context, options client.ScheduleUpdateOptions) error {
	f.update = &options
	return f.updateErr
}

func fakeSchedule() Schedule[any, FakeInput, string] {
	return Schedule[any, FakeInput, string]{
		ID: "fake-schedule",
		Workflow: Workflow[any, FakeInput, string]{
			Name:      "fake-workflow",
			TaskQueue: "X",
			Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
				return "run-result", nil
			},
		},
		Input:  FakeInput{TemporalWorkflowId: "fake-run"},
		Spec:   client.ScheduleSpec{CronExpressions: []string{"0 9 * * *"}},
		Policy: client.SchedulePolicies{Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP},
	}
}

func TestSchedule_Ensure_CreatesWhenMissing(t *testing.T) {
	schedules := &fakeScheduleClient{handle: &fakeScheduleHandle{}}
	sched := fakeSchedule()

	require.NoError(t, sched.Ensure(context.Background(), fakeTemporalClient{schedules: schedules}))

	require.NotNil(t, schedules.created)
	assert.Equal(t, "fake-schedule", schedules.created.ID)
	assert.Equal(t, []string{"0 9 * * *"}, schedules.created.Spec.CronExpressions)
	assert.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, schedules.created.Overlap)
	action, ok := schedules.created.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	assert.Equal(t, "fake-run", action.ID)
	assert.Equal(t, "fake-workflow", action.Workflow)
	assert.Equal(t, "X", action.TaskQueue)
	assert.Equal(t, []any{FakeInput{TemporalWorkflowId: "fake-run"}}, action.Args)
	assert.Empty(t, schedules.getHandleId, "an existing schedule should not be looked up after a successful create")
	assert.Nil(t, schedules.handle.update)
}

func TestSchedule_Ensure_UpdatesWhenPresent(t *testing.T) {
	handle := &fakeScheduleHandle{}
	schedules := &fakeScheduleClient{createErr: temporal.ErrScheduleAlreadyRunning, handle: handle}
	sched := fakeSchedule()

	require.NoError(t, sched.Ensure(context.Background(), fakeTemporalClient{schedules: schedules}))

	assert.Equal(t, "fake-schedule", schedules.getHandleId)
	require.NotNil(t, handle.update)
	require.NotNil(t, handle.update.DoUpdate)

	// Drive the update callback with what Temporal would describe: an older definition that an
	// operator has paused. The code-owned parts are replaced; the operator's state survives.
	existing := client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
		Action: &client.ScheduleWorkflowAction{ID: "old-run", Workflow: "fake-workflow"},
		Spec:   &client.ScheduleSpec{CronExpressions: []string{"0 6 * * *"}},
		Policy: &client.SchedulePolicies{Overlap: enums.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL},
		State:  &client.ScheduleState{Paused: true, Note: "paused by operator"},
	}}}
	update, err := handle.update.DoUpdate(existing)
	require.NoError(t, err)
	require.NotNil(t, update.Schedule)
	assert.Equal(t, []string{"0 9 * * *"}, update.Schedule.Spec.CronExpressions)
	assert.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, update.Schedule.Policy.Overlap)
	action, ok := update.Schedule.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	assert.Equal(t, "fake-run", action.ID)
	assert.Equal(t, "fake-workflow", action.Workflow)
	assert.Equal(t, "X", action.TaskQueue)
	require.NotNil(t, update.Schedule.State)
	assert.True(t, update.Schedule.State.Paused)
	assert.Equal(t, "paused by operator", update.Schedule.State.Note)
}

func TestSchedule_Ensure_ReturnsErrors(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		schedules := &fakeScheduleClient{createErr: assert.AnError, handle: &fakeScheduleHandle{}}
		err := fakeSchedule().Ensure(context.Background(), fakeTemporalClient{schedules: schedules})
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), `creating schedule "fake-schedule"`)
	})
	t.Run("update", func(t *testing.T) {
		schedules := &fakeScheduleClient{createErr: temporal.ErrScheduleAlreadyRunning, handle: &fakeScheduleHandle{updateErr: assert.AnError}}
		err := fakeSchedule().Ensure(context.Background(), fakeTemporalClient{schedules: schedules})
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), `updating schedule "fake-schedule"`)
	})
}

func TestSchedule_Register_WithoutClientRegistersWorkflowOnly(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	sched := fakeSchedule()
	sched.Register(struct{}{}, env)

	env.ExecuteWorkflow(sched.Workflow.Name, FakeInput{})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "run-result", result)
}

func TestSchedule_Register_ReconcilesWithClient(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	schedules := &fakeScheduleClient{handle: &fakeScheduleHandle{}}

	sched := fakeSchedule()
	sched.Client = func(cfg any) client.Client { return fakeTemporalClient{schedules: schedules} }
	sched.Register(struct{}{}, env)

	require.NotNil(t, schedules.created)
	assert.Equal(t, "fake-schedule", schedules.created.ID)
}
