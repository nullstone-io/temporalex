package temporalex

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

var (
	testTelemetryOnce sync.Once
	testMetricReader  *sdkmetric.ManualReader
	testSpanExporter  *tracetest.InMemoryExporter
)

// testTelemetry installs in-memory metric and trace providers as the OTel globals.
// The global providers delegate exactly once, so this is done once for the package and tests
// isolate themselves by workflow/activity name; counters are cumulative across tests.
func testTelemetry(t *testing.T) (*sdkmetric.ManualReader, *tracetest.InMemoryExporter) {
	t.Helper()
	testTelemetryOnce.Do(func() {
		testMetricReader = sdkmetric.NewManualReader()
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(testMetricReader)))
		testSpanExporter = tracetest.NewInMemoryExporter()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(testSpanExporter)))
	})
	return testMetricReader, testSpanExporter
}

// dataPoints returns the datapoints of the named counter whose attributes include all of `match`
func dataPoints(t *testing.T, reader *sdkmetric.ManualReader, metricName string, match ...attribute.KeyValue) []metricdata.DataPoint[int64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	found := make([]metricdata.DataPoint[int64], 0)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
		points:
			for _, dp := range sum.DataPoints {
				for _, kv := range match {
					if v, ok := dp.Attributes.Value(kv.Key); !ok || v != kv.Value {
						continue points
					}
				}
				found = append(found, dp)
			}
		}
	}
	return found
}

func attrsOf(dp metricdata.DataPoint[int64]) map[string]string {
	result := map[string]string{}
	for _, kv := range dp.Attributes.ToSlice() {
		result[string(kv.Key)] = kv.Value.Emit()
	}
	return result
}

func spanNamed(exporter *tracetest.InMemoryExporter, name string) (tracetest.SpanStub, bool) {
	for _, s := range exporter.GetSpans() {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

func spanAttrs(s tracetest.SpanStub) map[string]string {
	result := map[string]string{}
	for _, kv := range s.Attributes {
		result[string(kv.Key)] = kv.Value.Emit()
	}
	return result
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

// A user failure is classified on the workflow's span and counted as a user failure of its category
func TestWorkflow_RecordsUserFailure(t *testing.T) {
	reader, exporter := testTelemetry(t)
	const name = "record-user-failure"

	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", WithFailure(errors.New("Error: Unsupported argument"), FailureInfo{
				Class: FailureClassUser, Category: CategoryTerraformPlan, Code: "Error: Unsupported argument",
			})
		},
	}
	require.Error(t, runWorkflow(t, wflow, nil))

	span, ok := spanNamed(exporter, name+".Run")
	require.True(t, ok, "expected a %s.Run span", name)
	attrs := spanAttrs(span)
	assert.Equal(t, "user", attrs[AttrFailureClass])
	assert.Equal(t, CategoryTerraformPlan, attrs[AttrFailureCategory])
	assert.Equal(t, "Error: Unsupported argument", attrs[AttrFailureCode])
	assert.Equal(t, "*temporalex.ClassifiedError", attrs[AttrErrorType])
	assert.Equal(t, "true", attrs["testing"], "input span attributes are kept")

	failures := dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, name))
	require.Len(t, failures, 1)
	assert.Equal(t, int64(1), failures[0].Value)
	assert.Equal(t, map[string]string{
		MetricAttrClass:        "user",
		MetricAttrCategory:     CategoryTerraformPlan,
		MetricAttrWorkflowType: name,
		MetricAttrRoot:         "true",
	}, attrsOf(failures[0]))

	completions := dataPoints(t, reader, MetricWorkflowCompletions, attribute.String(MetricAttrWorkflowType, name))
	require.Len(t, completions, 1)
	assert.Equal(t, CompletionFailed, attrsOf(completions[0])[MetricAttrStatus])
}

// An unrecognised error is the default: internal/unknown. This is what pages for a regression.
func TestWorkflow_UnknownErrorIsInternal(t *testing.T) {
	reader, exporter := testTelemetry(t)
	const name = "record-unknown-failure"

	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", errors.New("pg: can't find column=package_mode in model=Deploy")
		},
	}
	require.Error(t, runWorkflow(t, wflow, nil))

	span, ok := spanNamed(exporter, name+".Run")
	require.True(t, ok)
	attrs := spanAttrs(span)
	assert.Equal(t, "internal", attrs[AttrFailureClass])
	assert.Equal(t, CategoryUnknown, attrs[AttrFailureCategory])
	assert.Empty(t, attrs[AttrFailureCode])

	failures := dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, name))
	require.Len(t, failures, 1)
	assert.Equal(t, "internal", attrsOf(failures[0])[MetricAttrClass])
	assert.Equal(t, CategoryUnknown, attrsOf(failures[0])[MetricAttrCategory])
}

// The classification is what PostRun returns, not the raw error (PostRun deciphers the error)
func TestWorkflow_ClassifiesPostRunError(t *testing.T) {
	reader, _ := testTelemetry(t)
	const name = "record-postrun-failure"

	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", errors.New("raw")
		},
		PostRun: func(wctx workflow.Context, input FakeInput, result string, err error) (string, error) {
			return result, WithFailure(err, FailureInfo{Class: FailureClassInternal, Category: CategoryDatabase})
		},
	}
	require.Error(t, runWorkflow(t, wflow, nil))

	failures := dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, name))
	require.Len(t, failures, 1)
	assert.Equal(t, CategoryDatabase, attrsOf(failures[0])[MetricAttrCategory])
}

// A cancellation is a completion, not a failure: it must never count towards failure metrics
func TestWorkflow_CancellationIsNotAFailure(t *testing.T) {
	reader, exporter := testTelemetry(t)
	const name = "record-cancellation"

	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			wctx.Done().Receive(wctx, nil)
			return "", wctx.Err()
		},
	}
	err := runWorkflow(t, wflow, func(env *testsuite.TestWorkflowEnvironment) {
		env.RegisterDelayedCallback(env.CancelWorkflow, 0)
	})
	require.Error(t, err)

	span, ok := spanNamed(exporter, name+".Run")
	require.True(t, ok)
	assert.Equal(t, "cancelled", spanAttrs(span)[AttrFailureClass])
	assert.Empty(t, spanAttrs(span)[AttrFailureCategory])

	assert.Empty(t, dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, name)))
	completions := dataPoints(t, reader, MetricWorkflowCompletions, attribute.String(MetricAttrWorkflowType, name))
	require.Len(t, completions, 1)
	assert.Equal(t, CompletionCancelled, attrsOf(completions[0])[MetricAttrStatus])
}

func TestWorkflow_SuccessIsCounted(t *testing.T) {
	reader, exporter := testTelemetry(t)
	const name = "record-success"

	wflow := Workflow[any, FakeInput, string]{
		Name:      name,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "ok", nil
		},
	}
	require.NoError(t, runWorkflow(t, wflow, nil))

	span, ok := spanNamed(exporter, name+".Run")
	require.True(t, ok)
	_, hasClass := spanAttrs(span)[AttrFailureClass]
	assert.False(t, hasClass, "a successful workflow carries no failure attributes")

	assert.Empty(t, dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, name)))
	completions := dataPoints(t, reader, MetricWorkflowCompletions, attribute.String(MetricAttrWorkflowType, name))
	require.Len(t, completions, 1)
	assert.Equal(t, CompletionSucceeded, attrsOf(completions[0])[MetricAttrStatus])
}

// A child workflow's failure is counted with root=false so dashboards can count one failure per intent
func TestWorkflow_ChildFailureIsNotRoot(t *testing.T) {
	reader, _ := testTelemetry(t)
	const childName = "record-child-failure"
	const parentName = "record-child-failure-parent"

	child := Workflow[any, FakeInput, string]{
		Name:      childName,
		TaskQueue: "X",
		Run: func(wctx workflow.Context, ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", WithFailure(errors.New("bad Dockerfile"), FailureInfo{Class: FailureClassUser, Category: CategoryDockerBuild})
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

	childFailures := dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, childName))
	require.Len(t, childFailures, 1)
	assert.Equal(t, "false", attrsOf(childFailures[0])[MetricAttrRoot])
	assert.Equal(t, CategoryDockerBuild, attrsOf(childFailures[0])[MetricAttrCategory])

	// The parent returned the child's error as is, so it keeps the child's classification (through the boundary)
	parentFailures := dataPoints(t, reader, MetricWorkflowFailures, attribute.String(MetricAttrWorkflowType, parentName))
	require.Len(t, parentFailures, 1)
	assert.Equal(t, "true", attrsOf(parentFailures[0])[MetricAttrRoot])
	assert.Equal(t, "user", attrsOf(parentFailures[0])[MetricAttrClass])
	assert.Equal(t, CategoryDockerBuild, attrsOf(parentFailures[0])[MetricAttrCategory])
}

// An activity failure is classified and counted once, after PostRun, and the classification survives to the workflow
func TestActivity_RecordsFailure(t *testing.T) {
	reader, _ := testTelemetry(t)
	const name = "activity/record-failure"

	act := Activity[any, FakeInput, string]{
		Name:    name,
		Options: DefaultActivityOptions("X"),
		Run: func(ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", WithFailure(errors.New("deployment failed"), FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout})
		},
	}
	scaffold := ActivityTestScaffold{T: t}
	scaffold.Env = scaffold.NewTestActivityEnvironment()
	act.Register(struct{}{}, scaffold)
	_, err := scaffold.Env.ExecuteActivity(name, FakeInput{})
	require.Error(t, err)
	assert.Equal(t, FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout}, Classify(err), "classification survives the boundary without a PostRun wrapping it")

	failures := dataPoints(t, reader, MetricActivityFailures, attribute.String(MetricAttrActivityType, name))
	require.Len(t, failures, 1)
	assert.Equal(t, int64(1), failures[0].Value)
	assert.Equal(t, map[string]string{
		MetricAttrClass:        "user",
		MetricAttrCategory:     CategoryDeployRollout,
		MetricAttrActivityType: name,
	}, attrsOf(failures[0]))
}

// A registered self-classifying type (the shell execution error pattern) needs no WithFailure
func TestActivity_RecordsRegisteredTypeFailure(t *testing.T) {
	reader, _ := testTelemetry(t)
	const name = "activity/record-registered-failure"

	act := Activity[any, FakeInput, string]{
		Name:    name,
		Options: DefaultActivityOptions("X"),
		Run: func(ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", &selfClassifiedError{Detail: "stderr", Failure: FailureInfo{Class: FailureClassUser, Category: CategoryTerraformApply, Code: "exit 1"}}
		},
	}
	scaffold := ActivityTestScaffold{T: t}
	scaffold.Env = scaffold.NewTestActivityEnvironment()
	act.Register(struct{}{}, scaffold)
	_, err := scaffold.Env.ExecuteActivity(name, FakeInput{})
	require.Error(t, err)

	var got *selfClassifiedError
	_, _, unwrapped := UnwrapError(err)
	require.ErrorAs(t, unwrapped, &got, "the registered type is re-hydrated")
	assert.Equal(t, "stderr", got.Detail)

	failures := dataPoints(t, reader, MetricActivityFailures, attribute.String(MetricAttrActivityType, name))
	require.Len(t, failures, 1)
	assert.Equal(t, CategoryTerraformApply, attrsOf(failures[0])[MetricAttrCategory])
}

// A cancelled activity is not a failure
func TestActivity_CancellationIsNotAFailure(t *testing.T) {
	reader, _ := testTelemetry(t)
	const name = "activity/record-cancellation"

	act := Activity[any, FakeInput, string]{
		Name:    name,
		Options: DefaultActivityOptions("X"),
		Run: func(ctx context.Context, cfg any, input FakeInput) (string, error) {
			return "", ErrSystemCancellation
		},
	}
	scaffold := ActivityTestScaffold{T: t}
	scaffold.Env = scaffold.NewTestActivityEnvironment()
	act.Register(struct{}{}, scaffold)
	_, err := scaffold.Env.ExecuteActivity(name, FakeInput{})
	require.Error(t, err)

	assert.Empty(t, dataPoints(t, reader, MetricActivityFailures, attribute.String(MetricAttrActivityType, name)))
}
