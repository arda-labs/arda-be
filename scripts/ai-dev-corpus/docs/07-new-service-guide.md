# Hướng dẫn triển khai service mới

## Tạo service từ template

Sử dụng skill new-service để sinh khung Go microservice theo convention của arda-be. Khung gồm: cmd/{service}, internal/{handler,repository,config}, migrations, Dockerfile, configs. Service mới phải đăng ký trong CI matrix và file kustomize của arda-infra.

## Quy tắc bắt buộc

Handler không được import internal/repository - script check-layering.mjs sẽ fail CI. Migration phải khớp mẫu {14 chữ số}_{tên-thường}.sql, có marker goose StatementBegin/End cho khối DO $$, không dùng DROP CASCADE, và phải idempotent vì nhiều bản sao chạy song song không có advisory lock.

## Cổng và giao thức

Từ đợt thống nhất cổng: HTTP lắng nghe 8080, gRPC lắng nghe 9090 trên mọi service. Frontend không gọi service trực tiếp - mọi request đi qua auth-gateway (BFF) ở cổng 8082 khi dev. Thêm endpoint HTTP mới nghĩa là phải thêm route tương ứng vào configs/policy.yaml của auth-gateway với id, path, methods, auth, risk và permissions.

## Driver cơ sở dữ liệu

arda-be dùng pgx/v5 chuẩn thư viện (đã loại lib/pq). Lưu ý pgx trả nil-slice thành NULL khác với pq trả '{}'. Helper tập trung ở ardapg.Driver. go.mod của service cần cả require lẫn replace cho các thư viện libs/go/*.

## Checklist trước khi merge

Chạy: node scripts/check-openapi.mjs cùng 12 script check-*.mjs khác, go test ./... trong từng thư mục có go.mod, docker compose config --quiet. Nếu sửa proto/arda phải chạy scripts/proto-generate.ps1. git diff --check phải sạch whitespace.
