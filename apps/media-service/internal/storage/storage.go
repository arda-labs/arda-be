package storage

import (
	"context"
	"io"
	"time"
)

type PresignPutInput struct {
	Bucket      string
	Key         string
	ContentType string
	ExpiresIn   time.Duration
}

type PresignGetInput struct {
	Bucket                     string
	Key                        string
	ExpiresIn                  time.Duration
	ResponseContentDisposition string
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
	PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string) error
	DeleteObject(ctx context.Context, bucket, key string) error
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

