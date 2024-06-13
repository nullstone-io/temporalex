package temporalex

import "go.temporal.io/sdk/workflow"

type TypedFuture[T any] struct {
	workflow.Future
	Result T
	Err    error
}

func (f TypedFuture[T]) AddToSelector(selector workflow.Selector, fns ...func(f TypedFuture[T]) T) {
	selector.AddFuture(f.Future, func(_ workflow.Future) {
		for _, fn := range fns {
			if fn != nil {
				fn(f)
			}
		}
	})
}

func ResolvedFuture[T any](wctx workflow.Context, t T, err error) TypedFuture[T] {
	future, setter := workflow.NewFuture(wctx)
	setter.Set(t, err)
	return TypedFuture[T]{
		Future: future,
		Result: t,
		Err:    err,
	}
}

type PostFunc[T any] func(wctx workflow.Context, t T, err error) (T, error)

func WrapFuture[T any](wctx workflow.Context, future workflow.Future, postFn PostFunc[T]) TypedFuture[T] {
	wrapped, setter := workflow.NewFuture(wctx)
	final := TypedFuture[T]{Future: wrapped}
	workflow.Go(wctx, func(wctx workflow.Context) {
		final.Err = future.Get(wctx, &final.Result)
		if postFn != nil {
			setter.Set(postFn(wctx, final.Result, final.Err))
		} else {
			setter.Set(final.Result, final.Err)
		}
	})
	return final
}
