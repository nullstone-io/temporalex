package temporalex

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	"testing"
)

func TestExternalWorkflow_DoChild(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	wflow := ExternalWorkflow[FakeInput, string]{
		Name:      "fake-workflow",
		TaskQueue: "X",
		HandleResult: func(wctx workflow.Context, result string, err error) (string, error) {
			assert.Equal(t, "sub-workflow", result)
			return "handled-result", nil
		},
	}

	mainWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		return wflow.DoChild(wctx, input)
	}
	subWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		return "sub-workflow", nil
	}
	env.RegisterWorkflow(mainWorkflow)
	env.RegisterWorkflowWithOptions(subWorkflow, workflow.RegisterOptions{
		Name:                          wflow.Name,
		DisableAlreadyRegisteredCheck: false,
	})

	input := FakeInput{}
	want := "handled-result"

	env.ExecuteWorkflow(mainWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, want, result)
}

func TestExternalWorkflow_DoChildAsync(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	wflow := ExternalWorkflow[FakeInput, string]{
		Name:      "fake-workflow",
		TaskQueue: "X",
		HandleResult: func(wctx workflow.Context, result string, err error) (string, error) {
			assert.Equal(t, "sub-workflow", result)
			return "handled-result", nil
		},
	}

	mainWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		future := wflow.DoChildAsync(wctx, input)
		return future.GetTyped(wctx)
	}
	subWorkflow := func(wctx workflow.Context, input FakeInput) (string, error) {
		return "sub-workflow", nil
	}
	env.RegisterWorkflow(mainWorkflow)
	env.RegisterWorkflowWithOptions(subWorkflow, workflow.RegisterOptions{
		Name:                          wflow.Name,
		DisableAlreadyRegisteredCheck: false,
	})

	input := FakeInput{}
	want := "handled-result"

	env.ExecuteWorkflow(mainWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, want, result)
}
