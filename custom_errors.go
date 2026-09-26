package temporalex

import (
	"errors"
	"go.temporal.io/sdk/temporal"
)

type UnwrapAppErrorFunc func(appErr *temporal.ApplicationError) error
type WrapErrorFunc func(err error) (error, bool)

type customErrorRegistryItem struct {
	UnwrapFunc UnwrapAppErrorFunc
	WrapFunc   WrapErrorFunc
	// DefaultFailure classifies errors of this type when the type does not implement FailureClassifier itself
	// (e.g. a type owned by another module). See Classify.
	DefaultFailure *FailureInfo
}

var customErrorRegistry = map[string]customErrorRegistryItem{}

// RegisterCustomError provides a centralized registry of custom errors that can be unwrapped using UnwrapCustomError
func RegisterCustomError(errType string, unwrapFn UnwrapAppErrorFunc, wrapFn WrapErrorFunc) {
	customErrorRegistry[errType] = customErrorRegistryItem{
		UnwrapFunc: unwrapFn,
		WrapFunc:   wrapFn,
	}
}

func RegisterCustomErrorDefault[T error](errType string) {
	RegisterCustomError(errType, DefaultUnwrap[T], DefaultWrap[T])
}

// RegisterCustomErrorDefaultWithFailure is RegisterCustomErrorDefault for a type that cannot implement
// FailureClassifier itself; every error of type T classifies as info unless it implements FailureClassifier
func RegisterCustomErrorDefaultWithFailure[T error](errType string, info FailureInfo) {
	customErrorRegistry[errType] = customErrorRegistryItem{
		UnwrapFunc:     DefaultUnwrap[T],
		WrapFunc:       DefaultWrap[T],
		DefaultFailure: &info,
	}
}

// registeredFailureInfo finds the default FailureInfo registered for the type of err, if any
func registeredFailureInfo(err error) (FailureInfo, bool) {
	for _, item := range customErrorRegistry {
		if item.DefaultFailure == nil || item.WrapFunc == nil {
			continue
		}
		if _, ok := item.WrapFunc(err); ok {
			return *item.DefaultFailure, true
		}
	}
	return FailureInfo{}, false
}

// UnwrapCustomError unwraps a temporal.ApplicationError into a detailed error
// This is useful for serializing errors with details other than the error message
func UnwrapCustomError(appErr *temporal.ApplicationError) error {
	if item, ok := customErrorRegistry[appErr.Type()]; ok && item.UnwrapFunc != nil {
		return item.UnwrapFunc(appErr)
	}
	if cause := appErr.Unwrap(); cause != nil {
		return cause
	}
	if msg := appErr.Message(); msg != "" {
		return errors.New(msg)
	}
	return appErr
}

type ErrorWrapper interface {
	WrapError() error
}

func WrapCustomError(err error) error {
	if err == nil {
		return nil
	}
	if temporal.IsApplicationError(err) {
		return err
	}
	if ce, ok := err.(ErrorWrapper); ok {
		return ce.WrapError()
	}
	// A classification wins over any registered type further down the chain: the registry is iterated in map
	// order, and losing the classification would page for a failure that was positively identified as the user's
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return temporal.NewApplicationErrorWithCause(ErrTypeClassifiedError, ErrTypeClassifiedError, classified, classified)
	}
	for name, item := range customErrorRegistry {
		if item.WrapFunc != nil {
			if finalErr, ok := item.WrapFunc(err); ok {
				return temporal.NewApplicationErrorWithCause(name, name, finalErr, finalErr)
			}
		}
	}
	return err
}

func DefaultUnwrap[T error](appErr *temporal.ApplicationError) error {
	var t T
	appErr.Details(&t)
	return t
}

func DefaultWrap[T error](err error) (error, bool) {
	var t T
	if errors.As(err, &t) {
		return t, true
	}
	return err, false
}
