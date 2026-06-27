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
DATABASE_DSN=postgres://arda_media:123456@localhost:5432/media?sslmode=disable
NATS_URL=nats://192.168.100.201:30222,nats://192.168.100.202:30222,nats://192.168.100.203:30222

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
POST /api/media/files/{file_id}/complete-upload
GET  /api/media/files/{file_id}
GET  /api/media/files/{file_id}/download-url
```

