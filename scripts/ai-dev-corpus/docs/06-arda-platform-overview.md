# Tổng quan nền tảng Arda

## Kiến trúc

Arda là một hệ sinh thái ứng dụng ngân hàng số gồm 4 kho chính: arda-be (15 microservice Go), arda-mfe (Module Federation: 1 shell + 14 remote Bun/Vite), arda-infra (GitOps kustomize + Argo CD) và arda-perf (k6 load test). Các service giao tiếp nội bộ qua gRPC, biên ngoài qua HTTP đi qua auth-gateway.

## Danh sách service

Các service nghiệp vụ: crm (khách hàng), finance (sổ cái kế toán), hrm (nhân sự), iam (danh tính và phân quyền), deposit (tiền gửi), loan (tín dụng), capital (vốn), mdm (danh mục nghiệp vụ), platform (tham chiếu hạ tầng), statistical (báo cáo), workflow (điều phối quy trình BPM trên Zeebe), media (tệp), notification (thông báo), ai (trợ lý và tri thức). auth-gateway là cổng BFF duy nhất cho frontend.

## Triển khai

Cluster K3s tự vận hành 3 node, chỉ một môi trường duy nhất. Mỗi lần push nhánh main của arda-be/arda-mfe, GitHub Actions build và đẩy image lên GHCR. Argo CD Image Updater ghi digest mới vào arda-infra, sau đó Argo CD tự động đồng bộ với prune và selfHeal. Vì selfHeal bật, mọi sửa đổi trực tiếp lên cluster bằng kubectl edit sẽ bị ghi đè - bắt buộc commit vào arda-infra.

## Cơ sở dữ liệu

PostgreSQL chạy bằng CloudNativePG trên namespace database, tên cụm pg-main, 3 bản sao. Mỗi service có database riêng, truy cập bằng NodePort 30432 từ ngoài. Migration dùng goose và phải idempotent vì nhiều bản sao chạy song song.

## Bảo mật

Xác thực người dùng cuối qua Hydra (OAuth2) và Kratos (danh tính), auth-gateway phiên dịch session thành header ủy quyền. Giao tiếp nội bộ giữa các service dùng chữ ký HMAC qua header x-service-auth với bí mật chung, và mTLS cho các luồng nhạy cảm.
