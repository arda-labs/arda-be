package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName                string        `yaml:"app_name"`
	HTTPAddr               string        `yaml:"http_addr"`
	GRPCAddr               string        `yaml:"grpc_addr"`
	LogLevel               string        `yaml:"log_level"`
	DatabaseDSN            string        `yaml:"database_dsn"`
	NATSURL                string        `yaml:"nats_url"`
	StorageProvider        string        `yaml:"storage_provider"`
	StorageEndpoint        string        `yaml:"storage_endpoint"`
	StorageRegion          string        `yaml:"storage_region"`
	StorageBucket          string        `yaml:"storage_bucket"`
	StorageAccessKey       string        `yaml:"storage_access_key"`
	StorageSecretKey       string        `yaml:"storage_secret_key"`
	StorageForcePathStyle  bool          `yaml:"storage_force_path_style"`
	UploadMaxSizeMB        int64         `yaml:"upload_max_size_mb"`
	PresignUploadTTL       time.Duration `yaml:"presign_upload_ttl"`
	PresignDownloadTTL     time.Duration `yaml:"presign_download_ttl"`
	RequireScanBeforeReady bool          `yaml:"require_scan_before_ready"`
}

func Load() Config {
	cfg := Config{
		AppName:               "media-service",
		HTTPAddr:              "0.0.0.0:8092",
		GRPCAddr:              "0.0.0.0:9092",
		LogLevel:              "info",
		DatabaseDSN:           "postgres://arda_media:123456@localhost:5432/media?sslmode=disable",
		NATSURL:               "",
		StorageProvider:       "garage-main",
		StorageEndpoint:       "",
		StorageRegion:         "garage",
		StorageBucket:         "media",
		StorageForcePathStyle: true,
		UploadMaxSizeMB:       100,
		PresignUploadTTL:      15 * time.Minute,
		PresignDownloadTTL:    5 * time.Minute,
	}

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		cfg.loadYAML(path)
	} else {
		for _, p := range []string{"configs/config.yaml", "../configs/config.yaml", "/etc/arda/media-service/config.yaml"} {
			if cfg.loadYAML(p) {
				break
			}
		}
	}

	envStr("APP_NAME", &cfg.AppName)
	envStr("HTTP_ADDR", &cfg.HTTPAddr)
	envStr("GRPC_ADDR", &cfg.GRPCAddr)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("DATABASE_DSN", &cfg.DatabaseDSN)
	envStr("MEDIA_DATABASE_DSN", &cfg.DatabaseDSN)
	envStr("NATS_URL", &cfg.NATSURL)
	envStr("MEDIA_NATS_URL", &cfg.NATSURL)

	envStr("STORAGE_PROVIDER", &cfg.StorageProvider)
	envStr("MEDIA_STORAGE_PROVIDER", &cfg.StorageProvider)
	envStr("STORAGE_ENDPOINT", &cfg.StorageEndpoint)
	envStr("MEDIA_STORAGE_ENDPOINT", &cfg.StorageEndpoint)
	envStr("GARAGE_ENDPOINT", &cfg.StorageEndpoint)
	envStr("STORAGE_REGION", &cfg.StorageRegion)
	envStr("MEDIA_STORAGE_REGION", &cfg.StorageRegion)
	envStr("GARAGE_REGION", &cfg.StorageRegion)
	envStr("STORAGE_BUCKET", &cfg.StorageBucket)
	envStr("MEDIA_STORAGE_BUCKET", &cfg.StorageBucket)
	envStr("GARAGE_BUCKET", &cfg.StorageBucket)
	envStr("STORAGE_ACCESS_KEY", &cfg.StorageAccessKey)
	envStr("MEDIA_STORAGE_ACCESS_KEY", &cfg.StorageAccessKey)
	envStr("GARAGE_ACCESS_KEY", &cfg.StorageAccessKey)
	envStr("STORAGE_SECRET_KEY", &cfg.StorageSecretKey)
	envStr("MEDIA_STORAGE_SECRET_KEY", &cfg.StorageSecretKey)
	envStr("GARAGE_SECRET_KEY", &cfg.StorageSecretKey)
	envBool("STORAGE_FORCE_PATH_STYLE", &cfg.StorageForcePathStyle)
	envBool("MEDIA_STORAGE_FORCE_PATH_STYLE", &cfg.StorageForcePathStyle)
	envBool("GARAGE_FORCE_PATH_STYLE", &cfg.StorageForcePathStyle)
	envInt64("UPLOAD_MAX_SIZE_MB", &cfg.UploadMaxSizeMB)
	envInt64("MEDIA_UPLOAD_MAX_SIZE_MB", &cfg.UploadMaxSizeMB)
	envDuration("PRESIGN_UPLOAD_TTL", &cfg.PresignUploadTTL)
	envDuration("MEDIA_PRESIGN_UPLOAD_TTL", &cfg.PresignUploadTTL)
	envDuration("PRESIGN_DOWNLOAD_TTL", &cfg.PresignDownloadTTL)
	envDuration("MEDIA_PRESIGN_DOWNLOAD_TTL", &cfg.PresignDownloadTTL)
	envBool("REQUIRE_SCAN_BEFORE_READY", &cfg.RequireScanBeforeReady)
	envBool("MEDIA_REQUIRE_SCAN_BEFORE_READY", &cfg.RequireScanBeforeReady)

	return cfg
}

func (c *Config) loadYAML(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	type rawConfig struct {
		AppName                string `yaml:"app_name"`
		HTTPAddr               string `yaml:"http_addr"`
		GRPCAddr               string `yaml:"grpc_addr"`
		LogLevel               string `yaml:"log_level"`
		DatabaseDSN            string `yaml:"database_dsn"`
		NATSURL                string `yaml:"nats_url"`
		StorageProvider        string `yaml:"storage_provider"`
		StorageEndpoint        string `yaml:"storage_endpoint"`
		StorageRegion          string `yaml:"storage_region"`
		StorageBucket          string `yaml:"storage_bucket"`
		StorageAccessKey       string `yaml:"storage_access_key"`
		StorageSecretKey       string `yaml:"storage_secret_key"`
		StorageForcePathStyle  *bool  `yaml:"storage_force_path_style"`
		UploadMaxSizeMB        int64  `yaml:"upload_max_size_mb"`
		PresignUploadTTL       string `yaml:"presign_upload_ttl"`
		PresignDownloadTTL     string `yaml:"presign_download_ttl"`
		RequireScanBeforeReady *bool  `yaml:"require_scan_before_ready"`
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "config: parse %s: %v\n", path, err)
		return false
	}
	setStr(raw.AppName, &c.AppName)
	setStr(raw.HTTPAddr, &c.HTTPAddr)
	setStr(raw.GRPCAddr, &c.GRPCAddr)
	setStr(raw.LogLevel, &c.LogLevel)
	setStr(raw.DatabaseDSN, &c.DatabaseDSN)
	setStr(raw.NATSURL, &c.NATSURL)
	setStr(raw.StorageProvider, &c.StorageProvider)
	setStr(raw.StorageEndpoint, &c.StorageEndpoint)
	setStr(raw.StorageRegion, &c.StorageRegion)
	setStr(raw.StorageBucket, &c.StorageBucket)
	setStr(raw.StorageAccessKey, &c.StorageAccessKey)
	setStr(raw.StorageSecretKey, &c.StorageSecretKey)
	if raw.StorageForcePathStyle != nil {
		c.StorageForcePathStyle = *raw.StorageForcePathStyle
	}
	if raw.UploadMaxSizeMB > 0 {
		c.UploadMaxSizeMB = raw.UploadMaxSizeMB
	}
	if raw.PresignUploadTTL != "" {
		if d, err := time.ParseDuration(raw.PresignUploadTTL); err == nil {
			c.PresignUploadTTL = d
		}
	}
	if raw.PresignDownloadTTL != "" {
		if d, err := time.ParseDuration(raw.PresignDownloadTTL); err == nil {
			c.PresignDownloadTTL = d
		}
	}
	if raw.RequireScanBeforeReady != nil {
		c.RequireScanBeforeReady = *raw.RequireScanBeforeReady
	}
	return true
}

func setStr(value string, target *string) {
	if value != "" {
		*target = value
	}
}

func envStr(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}

func envBool(key string, target *bool) {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			*target = parsed
		}
	}
}

func envInt64(key string, target *int64) {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err == nil && parsed > 0 {
			*target = parsed
		}
	}
}

func envDuration(key string, target *time.Duration) {
	if v := os.Getenv(key); v != "" {
		parsed, err := time.ParseDuration(v)
		if err == nil {
			*target = parsed
		}
	}
}
