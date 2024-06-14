package temporalex

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	"testing"
	"time"
)

func TestNewResolvedFuture(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	mainWorkflow := func(wctx workflow.Context, input any) (any, error) {
		good := NewResolvedFuture[string](wctx, "x", nil)
		var got1 string
		if assert.NoError(t, good.Get(wctx, &got1)) {
			assert.Equal(t, "x", got1)
		}
		assert.Equal(t, "x", good.Result)
		assert.Nil(t, good.Err)

		bad := NewResolvedFuture[string](wctx, "", fmt.Errorf("bad"))
		var got2 string
		assert.EqualError(t, bad.Get(wctx, &got2), "bad")
		assert.Equal(t, "", got2)
		assert.Equal(t, "", bad.Result)
		assert.EqualError(t, bad.Err, "bad")

		return input, nil
	}
	env.RegisterWorkflow(mainWorkflow)

	env.ExecuteWorkflow(mainWorkflow, struct{}{})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestNewTypedFuture(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	mainWorkflow := func(wctx workflow.Context, input any) (any, error) {
		future, setter := workflow.NewFuture(wctx)
		wrapped := NewFuture[string](future, func(wctx workflow.Context, result string, err error) (string, error) {
			return fmt.Sprintf("%s/y", result), err
		})

		workflow.Go(wctx, func(ctx workflow.Context) {
			workflow.Sleep(ctx, time.Second)
			setter.Set("x", nil)
		})

		got, gotErr := wrapped.GetTyped(wctx)
		if assert.NoError(t, gotErr) {
			assert.Equal(t, "x/y", got)
		}
		assert.Equal(t, "x/y", wrapped.Result)
		assert.NoError(t, gotErr)

		return input, nil
	}
	env.RegisterWorkflow(mainWorkflow)

	env.ExecuteWorkflow(mainWorkflow, struct{}{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestTypedFuture_AddToSelector(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	mainWorkflow := func(wctx workflow.Context, input any) (any, error) {
		f1 := NewFuture[any](workflow.NewTimer(wctx, time.Second), nil)
		f2 := NewResolvedFuture[any](wctx, "resolved", nil)
		selector := workflow.NewSelector(wctx)
		f1.AddToSelector(selector)
		f2.AddToSelector(selector)

		for !f1.IsReady() && !f2.IsReady() {
			selector.Select(wctx)
		}

		return input, nil
	}
	env.RegisterWorkflow(mainWorkflow)

	env.ExecuteWorkflow(mainWorkflow, struct{}{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
