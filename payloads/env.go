package payloads

import (
	"context"
	"os"
)

const (
	// S3BucketNameEnvVar and S3BucketRegionEnvVar name the bucket that backs S3Store.
	// GcsBucketNameEnvVar names the bucket that backs GcsStore; GCS bucket names are global,
	// so there is no region counterpart.
	//
	// These live here rather than in each service's config loader so that every service
	// participating in a payload handoff resolves the same bucket without coordination.
	// Both ends of a handoff must agree: the Ref one service writes is only readable by a
	// service pointed at the same backend.
	S3BucketNameEnvVar   = "S3_BUCKET_NAME"
	S3BucketRegionEnvVar = "S3_BUCKET_REGION"
	GcsBucketNameEnvVar  = "GCS_BUCKET_NAME"

	// DefaultPrefix namespaces stored payloads inside the bucket so a lifecycle rule can
	// target them without touching anything else the bucket holds.
	DefaultPrefix = "temporal-payloads/"
)

// NewStoreFromEnv builds whichever Store the environment is configured for: S3 when
// S3BucketNameEnvVar is set, otherwise GCS when GcsBucketNameEnvVar is set. S3 wins if both
// are set rather than failing, so a service mid-migration keeps working.
//
// It returns a nil Store when neither is set, so a service can run without one configured.
// Callers that then attempt a handoff get a clear "payload store is not configured" error
// from PutJSON/GetJSON rather than a nil dereference.
func NewStoreFromEnv(ctx context.Context) (Store, error) {
	if bucket := os.Getenv(S3BucketNameEnvVar); bucket != "" {
		return NewS3Store(ctx, bucket, os.Getenv(S3BucketRegionEnvVar))
	}
	if bucket := os.Getenv(GcsBucketNameEnvVar); bucket != "" {
		return NewGcsStore(ctx, bucket)
	}
	return nil, nil
}
