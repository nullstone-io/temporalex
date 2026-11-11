package temporalex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func TestExternalActivity_startOptions(t *testing.T) {
	retry := &temporal.RetryPolicy{MaximumAttempts: 1}
	activity := ExternalActivity[FakeInput, any]{
		Name:      "activity/fake",
		TaskQueue: "queue-a",
		Options: workflow.ActivityOptions{
			StartToCloseTimeout: time.Hour,
			RetryPolicy:         retry,
		},
	}

	t.Run("fills empty fields from the activity", func(t *testing.T) {
		got := activity.startOptions(client.StartActivityOptions{ID: "id-1"})
		assert.Equal(t, "id-1", got.ID)
		assert.Equal(t, "queue-a", got.TaskQueue)
		assert.Equal(t, time.Hour, got.StartToCloseTimeout)
		assert.Same(t, retry, got.RetryPolicy)
	})

	t.Run("keeps explicit fields", func(t *testing.T) {
		override := &temporal.RetryPolicy{MaximumAttempts: 5}
		got := activity.startOptions(client.StartActivityOptions{
			ID:                  "id-2",
			TaskQueue:           "queue-b",
			StartToCloseTimeout: time.Minute,
			RetryPolicy:         override,
		})
		assert.Equal(t, "queue-b", got.TaskQueue)
		assert.Equal(t, time.Minute, got.StartToCloseTimeout)
		assert.Same(t, override, got.RetryPolicy)
	})

	t.Run("falls back to the options task queue", func(t *testing.T) {
		a := activity
		a.TaskQueue = ""
		a.Options.TaskQueue = "queue-c"
		got := a.startOptions(client.StartActivityOptions{ID: "id-3"})
		assert.Equal(t, "queue-c", got.TaskQueue)
	})
}
