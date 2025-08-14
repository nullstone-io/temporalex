package temporalex

import (
	"github.com/nexus-rpc/sdk-go/nexus"
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

func (a WorkflowTestScaffold) RegisterDynamicWorkflow(wflow interface{}, options workflow.DynamicRegisterOptions) {
}
func (a WorkflowTestScaffold) RegisterDynamicActivity(activity interface{}, options activity.DynamicRegisterOptions) {
}
func (a WorkflowTestScaffold) RegisterNexusService(service *nexus.Service) {}
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
