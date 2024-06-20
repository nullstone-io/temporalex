package temporalex

import (
	"go.temporal.io/sdk/workflow"
	"log"
	"sync"
)

type OnResolvedFunc[T any] func(wctx workflow.Context, result T, err error) (T, error)

var _ workflow.Future = &TypedFuture[any]{}

type TypedFuture[T any] struct {
	baseFuture workflow.Future
	Result     T
	Err        error
	onResolved OnResolvedFunc[T]
	once       *sync.Once
}

func (f *TypedFuture[T]) Get(wctx workflow.Context, ptr any) error {
	f.baseFuture.Get(wctx, ptr)
	f.resolve(wctx)
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

func (f *TypedFuture[T]) AddToSelector(wctx workflow.Context, selector workflow.Selector, fns ...func(f TypedFuture[T]) T) {
	selector.AddFuture(f.baseFuture, func(bf workflow.Future) {
		f.resolve(wctx)
		for _, fn := range fns {
			if fn != nil {
				fn(*f)
			}
		}
	})
}

func (f *TypedFuture[T]) resolve(wctx workflow.Context) {
	f.once.Do(func() {
		log.Println("resolve")
		f.Err = f.baseFuture.Get(wctx, &f.Result)
		if f.onResolved != nil {
			f.Result, f.Err = f.onResolved(wctx, f.Result, f.Err)
		}
	})
}

func NewResolvedFuture[T any](wctx workflow.Context, result T, err error) *TypedFuture[T] {
	future, setter := workflow.NewFuture(wctx)
	setter.Set(result, err)
	tf := &TypedFuture[T]{
		Result:     result,
		Err:        err,
		onResolved: nil,
		baseFuture: future,
		once:       &sync.Once{},
	}
	tf.once.Do(func() {})
	return tf
}

func NewFuture[T any](future workflow.Future, onResolved OnResolvedFunc[T]) *TypedFuture[T] {
	return &TypedFuture[T]{
		baseFuture: future,
		onResolved: onResolved,
		once:       &sync.Once{},
	}
}
