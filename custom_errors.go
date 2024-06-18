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
