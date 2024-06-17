package temporalex

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"testing"
)

var _ worker.Registry = ActivityTestScaffold{}

type ActivityTestScaffold struct {
	testsuite.WorkflowTestSuite
	Env *testsuite.TestActivityEnvironment
	T   *testing.T
}

func (a ActivityTestScaffold) RegisterWorkflow(w interface{}) {}
func (a ActivityTestScaffold) RegisterWorkflowWithOptions(w interface{}, options workflow.RegisterOptions) {
}
func (a ActivityTestScaffold) RegisterActivity(act interface{}) {
	a.Env.RegisterActivity(act)
}
func (a ActivityTestScaffold) RegisterActivityWithOptions(act interface{}, options activity.RegisterOptions) {
	a.Env.RegisterActivityWithOptions(act, options)
}
