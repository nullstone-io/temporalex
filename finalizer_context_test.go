package temporalex

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// cleanupActivity stands in for a status write performed by a finalizer
func cleanupActivity(ctx context.Context, marker string) (string, error) {
	return marker, nil
}

func doCleanup(wctx workflow.Context, marker string) string {
	opts := workflow.ActivityOptions{StartToCloseTimeout: time.Minute}
	var result string
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(wctx, opts), cleanupActivity, marker).Get(wctx, &result); err != nil {
		return "skipped:" + marker
	}
	return result
}

func waitForCancellation(wctx workflow.Context) {
	selector := workflow.NewSelector(wctx)
	selector.AddReceive(wctx.Done(), func(c workflow.ReceiveChannel, more bool) {})
	selector.Select(wctx)
}

// A cancelled workflow's PostRun, and the parent's HandleResult for a cancelled child, still perform their cleanup activities
func TestFinalizerContext_WorkflowPostRunAndHandleResult(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(cleanupActivity)

	child := Workflow[any, FakeInput, string]{
		Name:      "child-workflow",
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			waitForCancellation(wctx)
			return "", wctx.Err()
		},
		PostRun: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			// The child finalizes on a context that outlives the cancellation
			return doCleanup(wctx, "post-run"), nil
		},
		HandleResult: func(wctx workflow.Context, ctx context.Context, input FakeInput, result string, err error) (string, error) {
			// The parent is cancelled too, so its finalizer must also run on a disconnected context
			return result + "," + doCleanup(wctx, "handle-result"), nil
		},
	}
	child.Register(struct{}{}, env)

	mainWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		return child.DoChild(wctx, context.TODO(), input)
	}
	env.RegisterWorkflow(mainWorkflow)
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Millisecond)

	env.ExecuteWorkflow(mainWorkflow, FakeInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "post-run,handle-result", result)
}

// An activity's HandleResult still performs its cleanup when the workflow is cancelled while the activity runs
func TestFinalizerContext_ActivityHandleResult(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(cleanupActivity)

	act := Activity[any, string, string]{
		Name:    "slow-activity",
		Options: workflow.ActivityOptions{StartToCloseTimeout: time.Minute, HeartbeatTimeout: time.Second},
		Run: func(ctx context.Context, cfg any, input string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
		HandleResult: func(wctx workflow.Context, input string, result string, err error) (string, error) {
			return doCleanup(wctx, "handle-result"), nil
		},
	}
	act.Register(struct{}{}, env)

	mainWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		return act.Do(wctx, "x")
	}
	env.RegisterWorkflow(mainWorkflow)
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Millisecond)

	env.ExecuteWorkflow(mainWorkflow, FakeInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "handle-result", result)
}
