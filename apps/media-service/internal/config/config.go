package config

import (
	"fmt"
	"os"
	"time"

	ardaconfig "github.com/arda-labs/arda/libs/go/arda-config"
)

// Config holds runtime configuration for media-service. Every value accepts a
// generic name, a MEDIA_-prefixed name and (for storage) a GARAGE_ name; the
// later alias wins. Empty strings and non-positive sizes never override the
// default (config:"positive").
type Config struct {
	AppName                string        `yaml:"app_name" env:"APP_NAME" config:"positive"`
	HTTPAddr               string        `yaml:"http_addr" env:"HTTP_ADDR" config:"positive"`
	GRPCAddr               string        `yaml:"grpc_addr" env:"GRPC_ADDR" config:"positive"`
	LogLevel               string        `yaml:"log_level" env:"LOG_LEVEL" config:"positive"`
	DatabaseDSN            string        `yaml:"database_dsn" env:"DATABASE_DSN,MEDIA_DATABASE_DSN" config:"positive"`
	NATSURL                string        `yaml:"nats_url" env:"NATS_URL,MEDIA_NATS_URL" config:"positive"`
	StorageProvider        string        `yaml:"storage_provider" env:"STORAGE_PROVIDER,MEDIA_STORAGE_PROVIDER" config:"positive"`
	StorageEndpoint        string        `yaml:"storage_endpoint" env:"STORAGE_ENDPOINT,MEDIA_STORAGE_ENDPOINT,GARAGE_ENDPOINT" config:"positive"`
	StoragePublicEndpoint  string        `yaml:"storage_public_endpoint" env:"STORAGE_PUBLIC_ENDPOINT,MEDIA_STORAGE_PUBLIC_ENDPOINT,GARAGE_PUBLIC_ENDPOINT" config:"positive"`
	StorageRegion          string        `yaml:"storage_region" env:"STORAGE_REGION,MEDIA_STORAGE_REGION,GARAGE_REGION" config:"positive"`
	StorageBucket          string        `yaml:"storage_bucket" env:"STORAGE_BUCKET,MEDIA_STORAGE_BUCKET,GARAGE_BUCKET" config:"positive"`
	StorageAccessKey       string        `yaml:"storage_access_key" env:"STORAGE_ACCESS_KEY,MEDIA_STORAGE_ACCESS_KEY,GARAGE_ACCESS_KEY" config:"positive"`
	StorageSecretKey       string        `yaml:"storage_secret_key" env:"STORAGE_SECRET_KEY,MEDIA_STORAGE_SECRET_KEY,GARAGE_SECRET_KEY" config:"positive"`
	StorageForcePathStyle  bool          `yaml:"storage_force_path_style" env:"STORAGE_FORCE_PATH_STYLE,MEDIA_STORAGE_FORCE_PATH_STYLE,GARAGE_FORCE_PATH_STYLE"`
	UploadMaxSizeMB        int64         `yaml:"upload_max_size_mb" env:"UPLOAD_MAX_SIZE_MB,MEDIA_UPLOAD_MAX_SIZE_MB" config:"positive"`
	StreamMaxSizeMB        int64         `yaml:"stream_max_size_mb" env:"STREAM_MAX_SIZE_MB,MEDIA_STREAM_MAX_SIZE_MB" config:"positive"`
	PreviewMaxSizeMB       int64         `yaml:"preview_max_size_mb" env:"PREVIEW_MAX_SIZE_MB,MEDIA_PREVIEW_MAX_SIZE_MB" config:"positive"`
	GotenbergURL           string        `yaml:"gotenberg_url" env:"GOTENBERG_URL,MEDIA_GOTENBERG_URL" config:"positive"`
	PresignUploadTTL       time.Duration `yaml:"presign_upload_ttl" env:"PRESIGN_UPLOAD_TTL,MEDIA_PRESIGN_UPLOAD_TTL"`
	PresignDownloadTTL     time.Duration `yaml:"presign_download_ttl" env:"PRESIGN_DOWNLOAD_TTL,MEDIA_PRESIGN_DOWNLOAD_TTL"`
	RequireScanBeforeReady bool          `yaml:"require_scan_before_ready" env:"REQUIRE_SCAN_BEFORE_READY,MEDIA_REQUIRE_SCAN_BEFORE_READY"`
	TempFileTTL            time.Duration `yaml:"temp_file_ttl" env:"TEMP_FILE_TTL,MEDIA_TEMP_FILE_TTL"`
	AllowedUploadMIME      []string      `yaml:"allowed_upload_mime" env:"ALLOWED_UPLOAD_MIME,MEDIA_ALLOWED_UPLOAD_MIME" config:"positive"`
}

func Load() Config {
	cfg := Config{
		AppName:               "media-service",
		HTTPAddr:              "0.0.0.0:8092",
		GRPCAddr:              "0.0.0.0:9090",
		LogLevel:              "info",
		DatabaseDSN:           "",
		NATSURL:               "",
		StorageProvider:       "garage-main",
		StorageEndpoint:       "",
		StorageRegion:         "garage",
		StorageBucket:         "media",
		StorageForcePathStyle: true,
		UploadMaxSizeMB:       100,
		StreamMaxSizeMB:       2,
		PreviewMaxSizeMB:      25,
		PresignUploadTTL:      15 * time.Minute,
		PresignDownloadTTL:    5 * time.Minute,
		TempFileTTL:           24 * time.Hour,
	}
	if err := ardaconfig.Load(&cfg, ardaconfig.ServiceOptions("media-service")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return cfg
}
