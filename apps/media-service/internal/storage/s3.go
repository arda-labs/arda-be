package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	client         *s3.Client
	presign        *s3.PresignClient
	endpoint       string
	region         string
	accessKey      string
	secretKey      string
	forcePathStyle bool
	httpClient     *http.Client
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
		client:         client,
		presign:        s3.NewPresignClient(client),
		endpoint:       strings.TrimRight(cfg.Endpoint, "/"),
		region:         cfg.Region,
		accessKey:      cfg.AccessKey,
		secretKey:      cfg.SecretKey,
		forcePathStyle: cfg.ForcePathStyle,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
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
	req, err := p.newSignedHeadRequest(ctx, bucket, key)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head object: %w", err)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head object: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return ObjectInfo{}, fmt.Errorf("head object returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return ObjectInfo{
		SizeBytes:   size,
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        resp.Header.Get("ETag"),
	}, nil
}

func (p *S3Provider) newSignedHeadRequest(ctx context.Context, bucket, key string) (*http.Request, error) {
	endpointURL, err := url.Parse(p.endpoint)
	if err != nil {
		return nil, err
	}

	requestPath := s3ObjectPath(bucket, key, p.forcePathStyle)
	endpointURL.Path = requestPath
	endpointURL.RawQuery = ""

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(nil)
	host := endpointURL.Host
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := fmt.Sprintf("HEAD\n%s\n\n%s\n%s\n%s", requestPath, canonicalHeaders, signedHeaders, payloadHash)
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, p.region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, scope, sha256Hex([]byte(canonicalRequest)))
	signature := hmacSHA256Hex(signingKey(p.secretKey, dateStamp, p.region, "s3"), stringToSign)
	authorization := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", p.accessKey, scope, signedHeaders, signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpointURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Host = host
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("Authorization", authorization)
	return req, nil
}

func s3ObjectPath(bucket, key string, forcePathStyle bool) string {
	escapedKey := escapeS3Key(key)
	if forcePathStyle {
		return "/" + url.PathEscape(bucket) + "/" + escapedKey
	}
	return "/" + escapedKey
}

func escapeS3Key(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func hmacSHA256Hex(key []byte, data string) string {
	return hex.EncodeToString(hmacSHA256(key, data))
}

func signingKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}
