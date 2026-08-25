# media-service

Media metadata and S3-compatible object storage gateway for Arda.

Phase 1 provides:

- metadata migrations
- Garage/S3 presigned upload URL
- complete upload by verifying object metadata
- short-lived presigned download URL
- health endpoints

Runtime config is read from `configs/config.yaml` and environment variables. Do not commit real S3 credentials.

Required local env example:

```env
DATABASE_DSN=<database-dsn-from-local-secret>
NATS_URL=nats://192.168.10.201:30222,nats://192.168.10.202:30222,nats://192.168.10.203:30222
GRPC_ADDR=0.0.0.0:9092
MEDIA_GRPC_ADDR=media-service:9092
ARDA_SERVICE_AUTH_SECRET=<local-workload-secret>
ARDA_GRPC_CA_FILE=<local-ca-file>
ARDA_GRPC_CERT_FILE=<media-certificate-file>
ARDA_GRPC_KEY_FILE=<media-or-caller-key-file>

STORAGE_ENDPOINT=https://s3.arda.io.vn
STORAGE_ACCESS_KEY=...
STORAGE_SECRET_KEY=...
STORAGE_BUCKET=media
STORAGE_REGION=garage
STORAGE_FORCE_PATH_STYLE=true
```

HTTP endpoints:

```txt
POST /api/media/files/init-upload
POST /api/media/files/{public_id}/complete-upload
POST /api/media                 (multipart upload)
GET  /api/media/{public_id}
GET  /api/media/{public_id}/download
DELETE /api/media/{public_id}
```

Internal file attachment is gRPC-only through `MediaService.AttachFiles` and
requires the shared mTLS plus signed workload identity contract. Callers must
not use the public HTTP attach route as an internal service shortcut.

