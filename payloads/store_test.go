package payloads

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	want := map[string]string{".nullstone/config.yml": "apps:\n  api:\n"}
	ref, err := PutJSON(ctx, store, want)
	require.NoError(t, err)
	require.False(t, ref.IsZero(), "a stored payload must produce a usable ref")

	got, err := GetJSON[map[string]string](ctx, store, ref)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMemoryStore_EachPutGetsItsOwnKey(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// An activity retry must not overwrite what a prior attempt stored -- its ref may
	// already be recorded in history.
	first, err := store.Put(ctx, []byte("attempt-1"))
	require.NoError(t, err)
	second, err := store.Put(ctx, []byte("attempt-2"))
	require.NoError(t, err)

	assert.NotEqual(t, first.Key, second.Key)

	got, err := store.Get(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, []byte("attempt-1"), got, "the first payload must survive the retry")
}

func TestGetJSON_ZeroRefYieldsZeroValue(t *testing.T) {
	// A workflow that never stored anything hands through a zero Ref; consumers should not
	// have to special-case it.
	got, err := GetJSON[map[string]string](context.Background(), NewMemoryStore(), Ref{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetJSON_MissingRef(t *testing.T) {
	_, err := GetJSON[map[string]string](context.Background(), NewMemoryStore(), Ref{Bucket: "memory", Key: "nope"})
	assert.ErrorContains(t, err, "not found")
}

func TestJSON_NilStore(t *testing.T) {
	// Services may run without a bucket configured. The failure should name the cause.
	_, err := PutJSON(context.Background(), nil, map[string]string{})
	assert.ErrorContains(t, err, "not configured")

	_, err = GetJSON[map[string]string](context.Background(), nil, Ref{Bucket: "b", Key: "k"})
	assert.ErrorContains(t, err, "not configured")
}

func TestMemoryStore_PutCopiesInput(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	data := []byte("original")
	ref, err := store.Put(ctx, data)
	require.NoError(t, err)
	copy(data, "mutated!")

	got, err := store.Get(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), got)
}
