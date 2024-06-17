package temporalex

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"testing"
)

var _ worker.Registry = WorkflowTestScaffold{}

type WorkflowTestScaffold struct {
	testsuite.WorkflowTestSuite
	Env *testsuite.TestWorkflowEnvironment
	T   *testing.T
}

func (a WorkflowTestScaffold) RegisterWorkflow(w interface{}) {
	a.Env.RegisterWorkflow(w)
}
func (a WorkflowTestScaffold) RegisterWorkflowWithOptions(w interface{}, options workflow.RegisterOptions) {
	a.Env.RegisterWorkflowWithOptions(w, options)
}
func (a WorkflowTestScaffold) RegisterActivity(act interface{}) {
	a.Env.RegisterActivity(act)
}
func (a WorkflowTestScaffold) RegisterActivityWithOptions(act interface{}, options activity.RegisterOptions) {
	a.Env.RegisterActivityWithOptions(act, options)
}
