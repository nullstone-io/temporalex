package temporalex

import (
	"fmt"
	"go.temporal.io/sdk/workflow"
	"reflect"
)

type OnResolvedFunc[T any] func(wctx workflow.Context, result T, err error) (T, error)

var _ workflow.Future = &TypedFuture[any]{}

type TypedFuture[T any] struct {
	Result     T
	Err        error
	OnResolved OnResolvedFunc[T]
	baseFuture workflow.Future
}

func (f *TypedFuture[T]) Get(ctx workflow.Context, ptr any) error {
	var result T
	if ptr != nil {
		f.Err = f.baseFuture.Get(ctx, ptr)
		if f.Err == nil {
			if ptrVal := reflect.ValueOf(ptr); ptrVal.Kind() != reflect.Ptr {
				return fmt.Errorf("valuePtr must be a pointer")
			} else if ptrElem, resultType := ptrVal.Elem(), reflect.TypeOf(f.Result); !ptrElem.Type().AssignableTo(resultType) {
				return fmt.Errorf("valuePtr is not assignable to %v", resultType)
			} else {
				result, _ = ptrElem.Interface().(T)
			}
		}
	} else {
		f.Err = f.baseFuture.Get(ctx, &result)
	}
	f.Result = result

	if f.OnResolved != nil {
		f.Result, f.Err = f.OnResolved(ctx, f.Result, f.Err)
	}
	return f.Err
}

func (f *TypedFuture[T]) IsReady() bool {
	return f.baseFuture.IsReady()
}

func (f *TypedFuture[T]) GetTyped(wctx workflow.Context) (T, error) {
	var t T
	f.Get(wctx, &t)
	return f.Result, f.Err
}

func (f *TypedFuture[T]) AddToSelector(selector workflow.Selector, fns ...func(f TypedFuture[T]) T) {
	selector.AddFuture(f, func(_ workflow.Future) {
		for _, fn := range fns {
			if fn != nil {
				fn(*f)
			}
		}
	})
}

func NewResolvedFuture[T any](wctx workflow.Context, result T, err error) *TypedFuture[T] {
	future, setter := workflow.NewFuture(wctx)
	setter.Set(result, err)
	return &TypedFuture[T]{
		Result:     result,
		Err:        err,
		OnResolved: nil,
		baseFuture: future,
	}
}

func NewFuture[T any](future workflow.Future, onResolved OnResolvedFunc[T]) *TypedFuture[T] {
	return &TypedFuture[T]{
		baseFuture: future,
		OnResolved: onResolved,
	}
}
