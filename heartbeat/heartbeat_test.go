package heartbeat

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nullstone-io/temporalex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestRun_recordsHeartbeatsWhileFnRuns(t *testing.T) {
	var beats atomic.Int32
	origIsActivity, origRecord := isActivity, recordHeartbeat
	isActivity = func(ctx context.Context) bool { return true }
	recordHeartbeat = func(ctx context.Context) { beats.Add(1) }
	t.Cleanup(func() { isActivity, recordHeartbeat = origIsActivity, origRecord })

	result, err := Run(context.Background(), 10*time.Millisecond, func(ctx context.Context) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "done", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "done", result)
	assert.GreaterOrEqual(t, beats.Load(), int32(3), "expected the immediate heartbeat plus ticks while fn ran")

	// The ticker must stop once fn returns
	settled := beats.Load()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, settled, beats.Load(), "heartbeats must stop after fn returns")
}

func TestRun_insideRealActivityEnvironment(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestActivityEnvironment()

	act := func(ctx context.Context) (string, error) {
		return Run(ctx, 10*time.Millisecond, func(ctx context.Context) (string, error) {
			time.Sleep(30 * time.Millisecond)
			return "done", nil
		})
	}
	env.RegisterActivity(act)

	val, err := env.ExecuteActivity(act)
	require.NoError(t, err)
	var result string
	require.NoError(t, val.Get(&result))
	assert.Equal(t, "done", result)
}

func TestRun_outsideActivityJustRunsFn(t *testing.T) {
	called := false
	result, err := Run(context.Background(), time.Millisecond, func(ctx context.Context) (int, error) {
		called = true
		return 42, nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, 42, result)
}

func TestActivityOptions(t *testing.T) {
	opts := ActivityOptions("queue", 20*time.Second)
	assert.Equal(t, "queue", opts.TaskQueue)
	assert.Equal(t, 60*time.Second, opts.HeartbeatTimeout)
	assert.Equal(t, temporalex.DefaultActivityOptions("queue").StartToCloseTimeout, opts.StartToCloseTimeout)

	assert.Equal(t, 3*DefaultInterval, ActivityOptions("queue", 0).HeartbeatTimeout)
}
