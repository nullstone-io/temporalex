package temporalex

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// detailedError stands in for a consumer's registered error type whose details must survive the boundary
type detailedError struct {
	Detail string `json:"detail"`
}

func (e *detailedError) Error() string { return "detailed: " + e.Detail }

// outcomeRecorder collects outcomes by workflow/activity type; observers are package-global, so tests
// register one recorder and filter by the unique names they use
type outcomeRecorder struct {
	mu         sync.Mutex
	workflows  map[string][]WorkflowOutcome
	activities map[string][]ActivityOutcome
}

var recorder = func() *outcomeRecorder {
	r := &outcomeRecorder{workflows: map[string][]WorkflowOutcome{}, activities: map[string][]ActivityOutcome{}}
	ObserveWorkflows(func(wctx workflow.Context, outcome WorkflowOutcome) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.workflows[outcome.WorkflowType] = append(r.workflows[outcome.WorkflowType], outcome)
	})
	ObserveActivities(func(ctx context.Context, outcome ActivityOutcome) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.activities[outcome.ActivityType] = append(r.activities[outcome.ActivityType], outcome)
	})
	RegisterCustomErrorDefault[*detailedError]("detailedError")
	return r
}()

func (r *outcomeRecorder) workflow(t *testing.T, name string) WorkflowOutcome {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.workflows[name], 1, "expected exactly one outcome for %s", name)
	return r.workflows[name][0]
}

func (r *outcomeRecorder) activity(t *testing.T, name string) ActivityOutcome {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.activities[name], 1, "expected exactly one outcome for %s", name)
	return r.activities[name][0]
}

func runWorkflow(t *testing.T, wflow Workflow[any, FakeInput, string], beforeRun func(env *testsuite.TestWorkflowEnvironment)) error {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	wflow.Register(struct{}{}, env)
	if beforeRun != nil {
		beforeRun(env)
	}
	env.ExecuteWorkflow(wflow.Name, FakeInput{TemporalWorkflowId: wflow.Name})
	require.True(t, env.IsWorkflowCompleted())
	return env.GetWorkflowError()
}

// A failed workflow's observers get its type, root flag and the error PostRun returned, with both spans
func TestObserveWorkflows_Failure(t *testing.T) {
	const name = "observe-failure"
	raw := errors.New("raw")
	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", raw
		},
		PostRun: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			return result, &detailedError{Detail: "deciphered"}
		},
	}
	require.Error(t, runWorkflow(t, wflow, nil))

	outcome := recorder.workflow(t, name)
	assert.Equal(t, name, outcome.WorkflowType)
	assert.True(t, outcome.Root)
	var detailed *detailedError
	require.ErrorAs(t, outcome.Err, &detailed, "observers see the error PostRun returned, before boundary wrapping")
	assert.Equal(t, "deciphered", detailed.Detail)
	assert.NotNil(t, outcome.Span)
	assert.NotNil(t, outcome.ParentSpan)
}

func TestObserveWorkflows_Success(t *testing.T) {
	const name = "observe-success"
	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "ok", nil
		},
	}
	require.NoError(t, runWorkflow(t, wflow, nil))
	assert.NoError(t, recorder.workflow(t, name).Err)
}

func TestObserveWorkflows_Cancellation(t *testing.T) {
	const name = "observe-cancellation"
	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			wctx.Done().Receive(wctx, nil)
			return "", wctx.Err()
		},
	}
	require.Error(t, runWorkflow(t, wflow, func(env *testsuite.TestWorkflowEnvironment) {
		env.RegisterDelayedCallback(env.CancelWorkflow, 0)
	}))
	errType, _, _ := UnwrapError(recorder.workflow(t, name).Err)
	assert.Equal(t, UnwrapErrTypeCancellation, errType)
}

// A child's outcome is not root, and its registered error reaches the parent with its details
// (the parent returned it as is), so the parent's outcome carries the same error
func TestObserveWorkflows_ChildAndParent(t *testing.T) {
	const childName = "observe-child"
	const parentName = "observe-child-parent"
	child := Workflow[any, FakeInput, string]{
		Name:      childName,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", &detailedError{Detail: "from child"}
		},
	}
	parent := Workflow[any, FakeInput, string]{
		Name:      parentName,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return child.DoChild(wctx, ctx, input)
		},
	}
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	parent.Register(struct{}{}, env)
	child.Register(struct{}{}, env)
	env.ExecuteWorkflow(parentName, FakeInput{TemporalWorkflowId: parentName})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())

	assert.False(t, recorder.workflow(t, childName).Root)
	parentOutcome := recorder.workflow(t, parentName)
	assert.True(t, parentOutcome.Root)
	_, _, unwrapped := UnwrapError(parentOutcome.Err)
	var detailed *detailedError
	require.ErrorAs(t, unwrapped, &detailed, "the child's registered error type is re-hydrated in the parent")
	assert.Equal(t, "from child", detailed.Detail)
}

// An activity's observers get its type and the error PostRun returned; the caller gets the boundary-wrapped error
func TestObserveActivities(t *testing.T) {
	const name = "activity/observe"
	act := Activity[any, FakeInput, string]{
		Name:    name,
		Options: DefaultActivityOptions("X"),
		Run: func(ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", &detailedError{Detail: "from activity"}
		},
	}
	scaffold := ActivityTestScaffold{T: t}
	scaffold.Env = scaffold.NewTestActivityEnvironment()
	act.Register(struct{}{}, scaffold)
	_, err := scaffold.Env.ExecuteActivity(name, FakeInput{})
	require.Error(t, err)

	outcome := recorder.activity(t, name)
	assert.Equal(t, name, outcome.ActivityType)
	var detailed *detailedError
	require.ErrorAs(t, outcome.Err, &detailed)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "detailedError", appErr.Type(), "the activity's registered error was wrapped for the boundary without a PostRun")
	_, _, unwrapped := UnwrapError(err)
	require.ErrorAs(t, unwrapped, &detailed)
	assert.Equal(t, "from activity", detailed.Detail)
}

type orderInner struct{ error }
type orderOuter struct{ error }

func (o orderOuter) Unwrap() error { return o.error }

// The registry wraps in registration order, so a consumer decides precedence by registering first
func TestWrapCustomError_RegistrationOrderWins(t *testing.T) {
	RegisterCustomError("orderInner", nil, func(err error) (error, bool) {
		var e orderInner
		return e, errors.As(err, &e)
	})
	RegisterCustomError("orderOuter", nil, func(err error) (error, bool) {
		var e orderOuter
		return e, errors.As(err, &e)
	})
	err := orderOuter{orderInner{errors.New("x")}}
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, WrapCustomError(err), &appErr)
	assert.Equal(t, "orderInner", appErr.Type())

	// Re-registering a name replaces it in place and keeps its position
	RegisterCustomError("orderInner", nil, func(err error) (error, bool) { return err, false })
	require.ErrorAs(t, WrapCustomError(err), &appErr)
	assert.Equal(t, "orderOuter", appErr.Type())
}
