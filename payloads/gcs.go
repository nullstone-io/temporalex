package payloads

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
)

var _ Store = GcsStore{}

// GcsStore keeps payloads in a Google Cloud Storage bucket.
//
// Like S3Store, the bucket should carry a lifecycle rule expiring stored payloads. Nothing
// deletes them otherwise: a Ref recorded in workflow history stays readable for as long as
// the workflow might replay, so the retention window should exceed the longest workflow
// execution plus whatever history retention the namespace is configured for.
type GcsStore struct {
	Client *storage.Client
	Bucket string
	// Prefix is prepended to every generated key. Empty by default, which writes to the
	// root of the bucket. Set it to scope payloads to a subpath -- useful when the bucket
	// holds anything else, so a lifecycle rule can target payloads alone.
	Prefix string
}

// NewGcsStore builds a GcsStore for the given bucket, using application default
// credentials. Unlike S3 there is no region to supply -- GCS bucket names are global.
func NewGcsStore(ctx context.Context, bucket string) (Store, error) {
	if bucket == "" {
		return nil, fmt.Errorf("payload store bucket is required")
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating gcs client for payload store: %w", err)
	}
	return GcsStore{
		Client: client,
		Bucket: bucket,
	}, nil
}

func (s GcsStore) Put(ctx context.Context, data []byte) (Ref, error) {
	// A fresh key per call, matching S3Store: activity retries store a new copy rather than
	// racing to overwrite one that an earlier attempt may still be reading.
	key := s.Prefix + uuid.NewString()

	w := s.Client.Bucket(s.Bucket).Object(key).NewWriter(ctx)
	if _, err := w.Write(data); err != nil {
		// Close reports the write error too, but abandoning the writer without closing it
		// leaks the resumable upload, so close and prefer the original error.
		w.Close()
		return Ref{}, fmt.Errorf("error storing payload in gs://%s/%s: %w", s.Bucket, key, err)
	}
	if err := w.Close(); err != nil {
		return Ref{}, fmt.Errorf("error storing payload in gs://%s/%s: %w", s.Bucket, key, err)
	}
	return Ref{Bucket: s.Bucket, Key: key}, nil
}

func (s GcsStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("cannot retrieve an empty payload reference")
	}
	bucket := ref.Bucket
	if bucket == "" {
		bucket = s.Bucket
	}

	r, err := s.Client.Bucket(bucket).Object(ref.Key).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("error retrieving payload from gs://%s/%s: %w", bucket, ref.Key, err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("error reading payload from gs://%s/%s: %w", bucket, ref.Key, err)
	}
	return data, nil
}
