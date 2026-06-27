package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Config struct {
	Endpoint       string
	Region         string
	AccessKey      string
	SecretKey      string
	ForcePathStyle bool
}

type S3Provider struct {
	client  *s3.Client
	presign *s3.PresignClient
}

func NewS3Provider(ctx context.Context, cfg S3Config) (*S3Provider, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("storage endpoint is required")
	}
	if cfg.Region == "" {
		cfg.Region = "garage"
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("storage access key and secret key are required")
	}

	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &S3Provider{
		client:  client,
		presign: s3.NewPresignClient(client),
	}, nil
}

func (p *S3Provider) PresignPutObject(ctx context.Context, input PresignPutInput) (PresignedURL, error) {
	expiresAt := time.Now().UTC().Add(input.ExpiresIn)
	out, err := p.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(input.Bucket),
		Key:         aws.String(input.Key),
		ContentType: aws.String(input.ContentType),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = input.ExpiresIn
	})
	if err != nil {
		return PresignedURL{}, fmt.Errorf("presign put object: %w", err)
	}
	headers := map[string]string{}
	if input.ContentType != "" {
		headers["Content-Type"] = input.ContentType
	}
	return PresignedURL{URL: out.URL, Headers: headers, ExpiresAt: expiresAt}, nil
}

func (p *S3Provider) PresignGetObject(ctx context.Context, input PresignGetInput) (PresignedURL, error) {
	expiresAt := time.Now().UTC().Add(input.ExpiresIn)
	out, err := p.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(input.Bucket),
		Key:    aws.String(input.Key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = input.ExpiresIn
	})
	if err != nil {
		return PresignedURL{}, fmt.Errorf("presign get object: %w", err)
	}
	return PresignedURL{URL: out.URL, ExpiresAt: expiresAt}, nil
}

func (p *S3Provider) HeadObject(ctx context.Context, bucket, key string) (ObjectInfo, error) {
	out, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head object: %w", err)
	}
	info := ObjectInfo{SizeBytes: aws.ToInt64(out.ContentLength), ETag: aws.ToString(out.ETag)}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	return info, nil
}

