package temporalex

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	"testing"
)

func TestWorkflow_DoChild(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	wflow := Workflow[any, FakeInput, string]{
		Name:      "fake-workflow",
		TaskQueue: "X",
		PostRun: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			assert.Equal(t, "run-result", result)
			return "post-run-result", nil
		},
		HandleResult: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			assert.Equal(t, "post-run-result", result)
			return "handled-result", nil
		},
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "run-result", nil
		},
	}
	wflow.Register(struct{}{}, env)

	mainWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		return wflow.DoChild(wctx, input)
	}
	env.RegisterWorkflow(mainWorkflow)

	input := FakeInput{}
	want := "handled-result"

	env.ExecuteWorkflow(mainWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, want, result)
}

func TestWorkflow_DoChildAsync(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	wflow := Workflow[any, FakeInput, string]{
		Name:      "fake-workflow",
		TaskQueue: "X",
		PostRun: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			assert.Equal(t, "run-result", result)
			return "post-run-result", nil
		},
		HandleResult: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			assert.Equal(t, "post-run-result", result)
			return "handled-result", nil
		},
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "run-result", nil
		},
	}
	wflow.Register(struct{}{}, env)

	mainWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		future := wflow.DoChildAsync(wctx, input)
		return future.GetTyped(wctx)
	}
	env.RegisterWorkflow(mainWorkflow)

	input := FakeInput{}
	want := "handled-result"

	env.ExecuteWorkflow(mainWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, want, result)
}
