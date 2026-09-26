package temporalex

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
)

// FailureClass says who (or what) caused a workflow or activity to fail.
//
// The default is internal: a failure is only ever attributed to the user when a producer positively
// identified it as such (see FailureClassifier, WithFailure, RegisterCustomErrorDefaultWithFailure).
// That default is deliberate: an unrecognised error is most likely a Nullstone regression, and it must
// page rather than blend in with customers' own failing builds and terraform runs.
type FailureClass string

const (
	// FailureClassUser is a failure caused by the customer's own code, configuration, or cloud account
	FailureClassUser FailureClass = "user"
	// FailureClassInternal is a Nullstone bug, an infrastructure fault, or an upstream dependency Nullstone relies on
	FailureClassInternal FailureClass = "internal"
	// FailureClassCancelled means the workflow/activity was cancelled (by a user or the system); it is not a failure
	FailureClassCancelled FailureClass = "cancelled"
	// FailureClassTimeout means the workflow/activity exceeded its timeout
	FailureClassTimeout FailureClass = "timeout"
)

// IsFailure reports whether this class counts towards failure metrics; cancellations and timeouts do not
func (c FailureClass) IsFailure() bool {
	return c == FailureClassUser || c == FailureClassInternal
}

// Failure categories form a bounded vocabulary shared by every service and by dashboards/alerts.
// Add to this list rather than inventing ad hoc strings so charts can group on them.
const (
	// user categories
	CategoryTerraformInit    = "terraform-init"
	CategoryTerraformPlan    = "terraform-plan"
	CategoryTerraformApply   = "terraform-apply"
	CategoryCloudCredentials = "cloud-credentials"
	CategoryConfig           = "config"
	CategoryModuleContract   = "module-contract"
	CategoryDependencyFailed = "dependency-failed"
	CategoryCheckout         = "checkout"
	CategoryDockerBuild      = "docker-build"
	CategorySiteAssetsBuild  = "site-assets-build"
	CategoryGithubActions    = "github-actions"
	CategoryPushAuth         = "push-auth"
	CategoryDeployRollout    = "deploy-rollout"
	CategoryDeployTimeout    = "deploy-timeout"

	// internal categories
	CategoryDatabase       = "database"
	CategoryPanic          = "panic"
	CategoryApi            = "api"
	CategoryTemporal       = "temporal"
	CategoryProviderMirror = "provider-mirror"
	CategoryStateBackend   = "state-backend"
	CategoryDockerDaemon   = "docker-daemon"
	CategoryUpstreamGithub = "upstream-github"
	CategoryUpstreamCloud  = "upstream-cloud"
	// CategoryUnknown is the default for anything not positively classified
	CategoryUnknown = "unknown"
)

// Span attribute keys recorded on failed workflow/activity spans
const (
	AttrFailureClass    = "nullstone.failure.class"
	AttrFailureCategory = "nullstone.failure.category"
	AttrFailureCode     = "nullstone.failure.code"
	AttrErrorType       = "error.type"
)

// maxFailureCodeLen bounds Code so a terraform diagnostic or stderr excerpt never bloats a span or metric attribute
const maxFailureCodeLen = 128

// FailureInfo classifies a failure. Code is an optional short detail (a terraform diagnostic summary,
// an exit code, an eviction reason) and is bounded to maxFailureCodeLen characters.
type FailureInfo struct {
	Class    FailureClass `json:"class"`
	Category string       `json:"category"`
	Code     string       `json:"code,omitempty"`
}

// IsZero reports whether no classification was set at all
func (i FailureInfo) IsZero() bool {
	return i.Class == "" && i.Category == "" && i.Code == ""
}

// normalized fills blanks with the internal/unknown default and bounds Code
func (i FailureInfo) normalized() FailureInfo {
	if i.Class == "" {
		i.Class = FailureClassInternal
	}
	if i.Category == "" && i.Class.IsFailure() {
		i.Category = CategoryUnknown
	}
	if len(i.Code) > maxFailureCodeLen {
		i.Code = i.Code[:maxFailureCodeLen]
	}
	return i
}

// Attributes returns the span attributes for this failure; errType is the Go type of the underlying error
func (i FailureInfo) Attributes(errType string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(AttrFailureClass, string(i.Class)),
	}
	if i.Category != "" {
		attrs = append(attrs, attribute.String(AttrFailureCategory, i.Category))
	}
	if i.Code != "" {
		attrs = append(attrs, attribute.String(AttrFailureCode, i.Code))
	}
	if errType != "" {
		attrs = append(attrs, attribute.String(AttrErrorType, errType))
	}
	return attrs
}

// FailureClassifier is implemented by error types that know what caused them.
// Types that cross a Temporal boundary must also be registered (RegisterCustomErrorDefault) so the
// re-hydrated value still implements it.
type FailureClassifier interface {
	FailureInfo() FailureInfo
}

var unknownFailure = FailureInfo{Class: FailureClassInternal, Category: CategoryUnknown}

// Classify determines who caused err. It works on raw errors and on errors that crossed a Temporal
// boundary (it unwraps the Temporal skeleton first). Resolution order:
//  1. cancellation / timeout / panic, from UnwrapError
//  2. the error (or any error in its chain) implements FailureClassifier
//  3. the error matches a type registered with a default FailureInfo
//  4. otherwise internal/unknown
//
// A nil error returns the zero FailureInfo.
func Classify(err error) FailureInfo {
	info, _ := classify(err)
	return info
}

// classify is Classify plus the unwrapped error, for recording error.type
func classify(err error) (FailureInfo, error) {
	if err == nil {
		return FailureInfo{}, nil
	}
	errType, _, unwrapped := UnwrapError(err)
	switch errType {
	case UnwrapErrTypeCancellation:
		return FailureInfo{Class: FailureClassCancelled}, unwrapped
	case UnwrapErrTypeTimeout:
		return FailureInfo{Class: FailureClassTimeout}, unwrapped
	case UnwrapErrTypePanic:
		return FailureInfo{Class: FailureClassInternal, Category: CategoryPanic}, unwrapped
	}
	// An activity that returns its cancelled context's error (e.g. a docker build or rollout watch interrupted
	// by a user cancel) was cancelled, not failed. This only holds in-process: across a Temporal boundary the
	// chain is gone, and the workflow side reads the cancellation from Temporal itself.
	if errors.Is(unwrapped, context.Canceled) {
		return FailureInfo{Class: FailureClassCancelled}, unwrapped
	} else if errors.Is(unwrapped, context.DeadlineExceeded) {
		return FailureInfo{Class: FailureClassTimeout}, unwrapped
	}

	var classifier FailureClassifier
	if errors.As(unwrapped, &classifier) {
		return classifier.FailureInfo().normalized(), unwrapped
	}
	if info, ok := registeredFailureInfo(unwrapped); ok {
		return info.normalized(), unwrapped
	}
	return unknownFailure, unwrapped
}

// errorTypeName is the Go type of err, for the error.type span attribute
func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

// ErrTypeClassifiedError is the Temporal application error type that carries a ClassifiedError across a boundary
const ErrTypeClassifiedError = "temporalex.ClassifiedError"

// ClassifiedError attaches a FailureInfo to an arbitrary error (see WithFailure).
// Only Message and Info survive a Temporal boundary; the wrapped Err does not.
type ClassifiedError struct {
	Message string      `json:"message"`
	Info    FailureInfo `json:"info"`
	Err     error       `json:"-"`
}

// WithFailure classifies err without defining an error type for it.
//
// Use it for plain errors (fmt.Errorf, sentinel errors, third-party errors). Do not wrap an error whose type
// is registered with RegisterCustomError (e.g. a shell execution error): the classification would win across
// the Temporal boundary and the registered type's own details (status message, stderr) would be lost.
// Give such types a FailureInfo field and implement FailureClassifier instead.
func WithFailure(err error, info FailureInfo) error {
	if err == nil {
		return nil
	}
	return &ClassifiedError{Message: err.Error(), Info: info.normalized(), Err: err}
}

func (e *ClassifiedError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *ClassifiedError) Unwrap() error { return e.Err }

func (e *ClassifiedError) FailureInfo() FailureInfo { return e.Info }

func init() {
	RegisterCustomErrorDefault[*ClassifiedError](ErrTypeClassifiedError)
}
