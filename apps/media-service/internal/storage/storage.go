package storage

import (
	"context"
	"time"
)

type PresignPutInput struct {
	Bucket      string
	Key         string
	ContentType string
	ExpiresIn   time.Duration
}

type PresignGetInput struct {
	Bucket    string
	Key       string
	ExpiresIn time.Duration
}

type PresignedURL struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	SizeBytes   int64
	ContentType string
	ETag        string
}

type Provider interface {
	PresignPutObject(ctx context.Context, input PresignPutInput) (PresignedURL, error)
	PresignGetObject(ctx context.Context, input PresignGetInput) (PresignedURL, error)
	HeadObject(ctx context.Context, bucket, key string) (ObjectInfo, error)
}

