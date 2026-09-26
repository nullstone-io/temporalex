package temporalex

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// selfClassifiedError stands in for a producer type that knows its own cause (e.g. a shell execution error)
type selfClassifiedError struct {
	Detail  string      `json:"detail"`
	Failure FailureInfo `json:"failure"`
}

func (e *selfClassifiedError) Error() string            { return "self classified: " + e.Detail }
func (e *selfClassifiedError) FailureInfo() FailureInfo { return e.Failure }

// externalError stands in for a type owned by another module that cannot implement FailureClassifier
type externalError struct {
	Field string `json:"field"`
}

func (e externalError) Error() string { return "invalid field " + e.Field }

func init() {
	RegisterCustomErrorDefault[*selfClassifiedError]("selfClassifiedError")
	RegisterCustomErrorDefaultWithFailure[externalError]("externalError", FailureInfo{Class: FailureClassUser, Category: CategoryConfig})
}

func TestClassify(t *testing.T) {
	userTfPlan := FailureInfo{Class: FailureClassUser, Category: CategoryTerraformPlan, Code: "Error: Unsupported argument"}

	tests := map[string]struct {
		err  error
		want FailureInfo
	}{
		"nil": {
			err:  nil,
			want: FailureInfo{},
		},
		"plain error defaults to internal/unknown": {
			err:  errors.New("pg: can't find column=package_mode in model=Deploy"),
			want: unknownFailure,
		},
		"wrapped plain error defaults to internal/unknown": {
			err:  fmt.Errorf("error updating status: %w", errors.New("boom")),
			want: unknownFailure,
		},
		"workflow cancellation": {
			err:  workflow.ErrCanceled,
			want: FailureInfo{Class: FailureClassCancelled},
		},
		"system cancellation already deciphered": {
			err:  ErrSystemCancellation,
			want: FailureInfo{Class: FailureClassCancelled},
		},
		"cancelled error with details": {
			err:  temporal.NewCanceledError("the platform evicted the rollout"),
			want: FailureInfo{Class: FailureClassCancelled},
		},
		"deadline exceeded": {
			err:  workflow.ErrDeadlineExceeded,
			want: FailureInfo{Class: FailureClassTimeout},
		},
		"activity returned its cancelled context error": {
			err:  fmt.Errorf("docker build cancelled: %w", context.Canceled),
			want: FailureInfo{Class: FailureClassCancelled},
		},
		"activity returned its expired context error": {
			err:  fmt.Errorf("timed out waiting for run to complete: %w", context.DeadlineExceeded),
			want: FailureInfo{Class: FailureClassTimeout},
		},
		"panic": {
			err:  temporal.NewApplicationErrorWithCause("panic", "PanicError", nil),
			want: unknownFailure, // an application error named PanicError is not a Temporal panic
		},
		"self-classified type": {
			err:  &selfClassifiedError{Detail: "x", Failure: userTfPlan},
			want: userTfPlan,
		},
		"self-classified type wrapped in context": {
			err:  fmt.Errorf("running plan: %w", &selfClassifiedError{Detail: "x", Failure: userTfPlan}),
			want: userTfPlan,
		},
		"self-classified with blank class normalises to internal/unknown": {
			err:  &selfClassifiedError{Detail: "x"},
			want: unknownFailure,
		},
		"self-classified with class but no category gets unknown category": {
			err:  &selfClassifiedError{Detail: "x", Failure: FailureInfo{Class: FailureClassUser}},
			want: FailureInfo{Class: FailureClassUser, Category: CategoryUnknown},
		},
		"registered default for external type": {
			err:  externalError{Field: "vars"},
			want: FailureInfo{Class: FailureClassUser, Category: CategoryConfig},
		},
		"registered default applies through wrapping": {
			err:  fmt.Errorf("parsing iac: %w", externalError{Field: "vars"}),
			want: FailureInfo{Class: FailureClassUser, Category: CategoryConfig},
		},
		"WithFailure": {
			err:  WithFailure(errors.New("deployment failed"), FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout}),
			want: FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout},
		},
		"WithFailure bounds the code": {
			err:  WithFailure(errors.New("x"), FailureInfo{Class: FailureClassInternal, Category: CategoryDatabase, Code: strings.Repeat("a", 500)}),
			want: FailureInfo{Class: FailureClassInternal, Category: CategoryDatabase, Code: strings.Repeat("a", maxFailureCodeLen)},
		},
		"activity error wrapping a plain failure": {
			err:  temporal.NewApplicationErrorWithCause("activity failed", "", errors.New("boom")),
			want: unknownFailure,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, Classify(test.err))
		})
	}
}

func TestWithFailure_NilIsNil(t *testing.T) {
	assert.Nil(t, WithFailure(nil, FailureInfo{Class: FailureClassUser}))
}

func TestWithFailure_KeepsTheChain(t *testing.T) {
	sentinel := errors.New("deployment failed")
	err := WithFailure(fmt.Errorf("watching rollout: %w", sentinel), FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout})
	assert.ErrorIs(t, err, sentinel)
	assert.EqualError(t, err, "watching rollout: deployment failed")
}

func TestFailureInfo_Attributes(t *testing.T) {
	info := FailureInfo{Class: FailureClassUser, Category: CategoryDockerBuild, Code: "exit 1"}
	assert.ElementsMatch(t, []attribute.KeyValue{
		attribute.String(AttrFailureClass, "user"),
		attribute.String(AttrFailureCategory, "docker-build"),
		attribute.String(AttrFailureCode, "exit 1"),
		attribute.String(AttrErrorType, "*errors.errorString"),
	}, info.Attributes(errorTypeName(errors.New("x"))))

	cancelled := FailureInfo{Class: FailureClassCancelled}
	assert.Equal(t, []attribute.KeyValue{attribute.String(AttrFailureClass, "cancelled")}, cancelled.Attributes(""))
}

// Classification must survive the Temporal boundary: an activity's error is serialized into an ApplicationError
// and re-hydrated in the workflow, and the workflow classifies what it gets back
func TestClassify_AcrossActivityBoundary(t *testing.T) {
	userTfPlan := FailureInfo{Class: FailureClassUser, Category: CategoryTerraformPlan, Code: "Error: Unsupported argument"}

	tests := map[string]struct {
		fn   func(ctx context.Context) error
		want FailureInfo
	}{
		"panic": {
			fn: func(ctx context.Context) error {
				var x *url.URL
				_ = x.String() // Intentional nil panic
				return nil
			},
			want: FailureInfo{Class: FailureClassInternal, Category: CategoryPanic},
		},
		"cancellation": {
			fn: func(ctx context.Context) error {
				return temporal.NewCanceledError("the platform evicted the rollout")
			},
			want: FailureInfo{Class: FailureClassCancelled},
		},
		"self-classified registered type": {
			fn: func(ctx context.Context) error {
				return WrapCustomError(&selfClassifiedError{Detail: "x", Failure: userTfPlan})
			},
			want: userTfPlan,
		},
		"registered type with default failure": {
			fn: func(ctx context.Context) error {
				return WrapCustomError(externalError{Field: "vars"})
			},
			want: FailureInfo{Class: FailureClassUser, Category: CategoryConfig},
		},
		"WithFailure on a plain error": {
			fn: func(ctx context.Context) error {
				return WrapCustomError(WithFailure(errors.New("deployment failed"), FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout}))
			},
			want: FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout},
		},
		"WithFailure without WrapCustomError loses its classification": {
			// The SDK serializes an unregistered error as its message only. Activity.run wraps for its callers
			// (see TestActivity_RecordsFailure); a raw activity function returning this must wrap itself.
			fn: func(ctx context.Context) error {
				return WithFailure(errors.New("deployment failed"), FailureInfo{Class: FailureClassUser, Category: CategoryDeployRollout})
			},
			want: unknownFailure,
		},
		"plain error": {
			fn: func(ctx context.Context) error {
				return errors.New("pg: can't find column=package_mode in model=Deploy")
			},
			want: unknownFailure,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			scaffold := ActivityTestScaffold{T: t}
			scaffold.Env = scaffold.NewTestActivityEnvironment()
			scaffold.Env.RegisterActivity(test.fn)
			_, gotErr := scaffold.Env.ExecuteActivity(test.fn)
			require.Error(t, gotErr)
			assert.Equal(t, test.want, Classify(gotErr))
		})
	}
}

// A classified error wrapping a registered type keeps its classification across the boundary,
// even though the registered type would otherwise have claimed the error
func TestWrapCustomError_ClassificationWins(t *testing.T) {
	inner := externalError{Field: "vars"}
	err := WrapCustomError(WithFailure(inner, FailureInfo{Class: FailureClassInternal, Category: CategoryDatabase}))
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, ErrTypeClassifiedError, appErr.Type())
	assert.Equal(t, FailureInfo{Class: FailureClassInternal, Category: CategoryDatabase}, Classify(err))
}

// An already-wrapped application error is left alone
func TestWrapCustomError_LeavesApplicationErrors(t *testing.T) {
	appErr := temporal.NewNonRetryableApplicationError("x", "CreateDeployError", errors.New("boom"))
	assert.Same(t, appErr, WrapCustomError(appErr))
}
