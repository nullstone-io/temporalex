package payloads

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

var _ Store = S3Store{}

// S3Store keeps payloads in an S3 bucket.
//
// The bucket should carry a lifecycle rule expiring stored payloads. Nothing deletes them
// otherwise: a Ref recorded in workflow history stays readable for as long as the workflow
// might replay, so the retention window should exceed the longest workflow execution plus
// whatever history retention the namespace is configured for.
type S3Store struct {
	Client *s3.Client
	Bucket string
	// Prefix is prepended to every generated key. Empty by default, which writes to the
	// root of the bucket. Set it to scope payloads to a subpath -- useful when the bucket
	// holds anything else, so a lifecycle rule can target payloads alone.
	Prefix string
}

// NewS3Store builds an S3Store for the given bucket. An empty region falls back to the
// ambient AWS config (AWS_REGION, instance metadata, and so on).
func NewS3Store(ctx context.Context, bucket, region string) (Store, error) {
	if bucket == "" {
		return nil, fmt.Errorf("payload store bucket is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error loading aws config for payload store: %w", err)
	}
	return S3Store{
		Client: s3.NewFromConfig(awsCfg),
		Bucket: bucket,
	}, nil
}

func (s S3Store) Put(ctx context.Context, data []byte) (Ref, error) {
	// A fresh key per call. Activity retries store a new copy rather than racing to
	// overwrite one that an earlier attempt may still be reading.
	key := s.Prefix + uuid.NewString()

	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return Ref{}, fmt.Errorf("error storing payload in s3://%s/%s: %w", s.Bucket, key, err)
	}
	return Ref{Bucket: s.Bucket, Key: key}, nil
}

func (s S3Store) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("cannot retrieve an empty payload reference")
	}
	bucket := ref.Bucket
	if bucket == "" {
		bucket = s.Bucket
	}

	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(ref.Key),
	})
	if err != nil {
		return nil, fmt.Errorf("error retrieving payload from s3://%s/%s: %w", bucket, ref.Key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading payload from s3://%s/%s: %w", bucket, ref.Key, err)
	}
	return data, nil
}
