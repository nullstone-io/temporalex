// Package payloads transfers large values between Temporal activities without putting
// them in the workflow payload.
//
// Temporal writes every activity input and output into workflow history, and the server
// rejects any payload above the namespace BlobSizeLimitError (2 MB by default) with
// TMPRL1103. A struct that is both the input and output of a chain of activities pays that
// cost once per activity boundary, so a single large field can fail a workflow that is
// otherwise well within limits.
//
// This package implements the claim-check pattern: the producing activity writes the value
// to external storage and returns a Ref, which is a handful of bytes. The consuming
// activity exchanges the Ref for the value. Only the Ref travels through history.
//
//	// in the producing activity
//	ref, err := payloads.PutJSON(ctx, cfg.PayloadStore, configFiles)
//
//	// in the consuming activity
//	configFiles, err := payloads.GetJSON[map[string]string](ctx, cfg.PayloadStore, ref)
//
// Put and Get perform network I/O, so they must be called from activities. Calling them
// from workflow code is non-deterministic and will break replay.
package payloads

import (
	"context"
	"encoding/json"
	"fmt"
)

// Ref locates a stored payload. It is deliberately small: a Ref is what travels through
// the Temporal payload in place of the value it stands for.
//
// Ref records the bucket alongside the key so that a payload stored before a bucket
// reconfiguration is still readable afterwards.
type Ref struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// IsZero reports whether the Ref points at nothing. A zero Ref is the natural
// representation of "this workflow never stored anything" -- callers should check it
// before calling Get.
func (r Ref) IsZero() bool {
	return r.Key == ""
}

func (r Ref) String() string {
	if r.IsZero() {
		return "<none>"
	}
	return fmt.Sprintf("%s/%s", r.Bucket, r.Key)
}

// Store reads and writes payloads held outside Temporal history.
//
// Implementations pick their own keys: the caller has no say in where a payload lands, only
// in the Ref it gets back. This keeps keys unique across activity retries, which matters
// because a retried activity stores a second copy rather than overwriting the first.
//
// Stored payloads are never deleted by this interface. Retries and replays make eager
// deletion unsafe -- a Ref recorded in history may be read long after the activity that
// produced it. Expire them with a storage lifecycle rule instead.
type Store interface {
	// Put writes data and returns a Ref that Get can exchange for it.
	Put(ctx context.Context, data []byte) (Ref, error)
	// Get returns the data previously written under ref.
	Get(ctx context.Context, ref Ref) ([]byte, error)
}

// PutJSON marshals v and stores it. It is the counterpart of GetJSON.
func PutJSON(ctx context.Context, store Store, v any) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("payload store is not configured")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return Ref{}, fmt.Errorf("error encoding payload: %w", err)
	}
	return store.Put(ctx, raw)
}

// GetJSON retrieves ref and unmarshals it into T.
// A zero Ref yields the zero value of T and no error, so a caller can hand through a Ref
// that was never populated without special-casing it.
func GetJSON[T any](ctx context.Context, store Store, ref Ref) (T, error) {
	var out T
	if ref.IsZero() {
		return out, nil
	}
	if store == nil {
		return out, fmt.Errorf("payload store is not configured")
	}
	raw, err := store.Get(ctx, ref)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("error decoding payload %s: %w", ref, err)
	}
	return out, nil
}
