package temporalex

import (
	"errors"
	"go.temporal.io/sdk/temporal"
)

type UnwrapAppErrorFunc func(appErr *temporal.ApplicationError) error
type WrapErrorFunc func(err error) (error, bool)

type customErrorRegistryItem struct {
	Name       string
	UnwrapFunc UnwrapAppErrorFunc
	WrapFunc   WrapErrorFunc
}

// customErrorRegistry keeps registration order: WrapCustomError tries items in that order and the first
// match wins, so a consumer decides precedence by registering first (a map would iterate randomly)
var (
	customErrorRegistry []customErrorRegistryItem
	customErrorIndex    = map[string]int{}
)

// RegisterCustomError provides a centralized registry of custom errors that can be unwrapped using UnwrapCustomError.
// Registering a type name again replaces the earlier registration in place.
func RegisterCustomError(errType string, unwrapFn UnwrapAppErrorFunc, wrapFn WrapErrorFunc) {
	item := customErrorRegistryItem{Name: errType, UnwrapFunc: unwrapFn, WrapFunc: wrapFn}
	if idx, ok := customErrorIndex[errType]; ok {
		customErrorRegistry[idx] = item
		return
	}
	customErrorIndex[errType] = len(customErrorRegistry)
	customErrorRegistry = append(customErrorRegistry, item)
}

func RegisterCustomErrorDefault[T error](errType string) {
	RegisterCustomError(errType, DefaultUnwrap[T], DefaultWrap[T])
}

// UnwrapCustomError unwraps a temporal.ApplicationError into a detailed error
// This is useful for serializing errors with details other than the error message
func UnwrapCustomError(appErr *temporal.ApplicationError) error {
	if idx, ok := customErrorIndex[appErr.Type()]; ok && customErrorRegistry[idx].UnwrapFunc != nil {
		return customErrorRegistry[idx].UnwrapFunc(appErr)
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

// WrapCustomError turns a registered error type (anywhere in err's chain) into an application error that carries
// its details across the Temporal boundary. Registered types are tried in registration order; the first match wins.
// An error that is already an application error is returned as is.
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
	for _, item := range customErrorRegistry {
		if item.WrapFunc != nil {
			if finalErr, ok := item.WrapFunc(err); ok {
				return temporal.NewApplicationErrorWithCause(item.Name, item.Name, finalErr, finalErr)
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
