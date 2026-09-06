# mdm-service

Master data management: các danh mục nghiệp vụ dùng chung cho platform Arda,
đối ứng với `be_epas_category` + các bảng `*Cfg*` rải trong module EPAS (xem
`docs/epas-survey/master-data-catalog.md`). Thay skeleton trống trước đây.

## Domain

| Catalog | Endpoint | Nội dung | Ghi chú |
|---|---|---|---|
| currencies | `/api/mdm/currencies` | Loại tiền (ISO + symbol + decimal_places) | seed 11 tiền tệ |
| countries | `/api/mdm/countries` | Quốc gia + quốc tịch | — |
| id-document-types | `/api/mdm/id-document-types` | Loại giấy tờ tùy thân (CCCD/CMND/hộ chiếu) | EPAS thiếu, bổ sung mới |
| collateral-types | `/api/mdm/collateral-types` | Loại tài sản đảm bảo (risk/deduction/guarantee ratio) | gom bản nhân bản 5 nơi của EPAS về 1 |
| loan-purposes | `/api/mdm/loan-purposes` | Mục đích vay + risk_level + hạn mức | — |
| fee-types | `/api/mdm/fee-types` | Loại phí (fixed/percent) | EPAS không có danh mục phí — mới |
| debt-groups | `/api/mdm/debt-groups` | Phân loại nợ theo Thông tư 02/2023/TT-NHNN (nhóm 1-5 + provisioning) | EPAS không có — mới |
| interest-rates | `/api/mdm/interest-rates` (+`/{id}/tiers`) | Lãi suất versioned: header + tier theo kỳ hiệu lực và bậc dư nợ | thay `CtgCfgInterestRate(_Dtl)` |
| economic-types | `/api/mdm/economic-types` | Loại hình kinh tế (SOE/PRIVATE/FDI...) | batch 2 |
| industries | `/api/mdm/industries` | Ngành nghề kinh doanh | batch 2 |
| loan-methods | `/api/mdm/loan-methods` | Phương thức cho vay | batch 2 |
| loan-contract-types | `/api/mdm/loan-contract-types` | Loại HĐ tín dụng (is_collateral) | batch 2 |
| fund-sources / fund-purposes | `/api/mdm/fund-sources|fund-purposes` | Loại nguồn vốn, mục đích sử dụng vốn | batch 2 (CFM P2 dùng) |
| base-rates | `/api/mdm/base-rates` | Lãi suất tham chiếu NHNN/thị trường theo nguồn | batch 2 |
| interest-factors | `/api/mdm/interest-factors` | Quy ước tử/mẫu số ngày tính lãi (360/365) | batch 2 |
| cash-denominations | `/api/mdm/cash-denominations` | Mệnh giá tiền theo chất liệu (VCM P2 dùng) | batch 2 |
| scoring-types / -indicator-groups / -indicators / -benchmarks | `/api/mdm/scoring-*` | Bộ chấm điểm: header → nhóm chỉ tiêu → chỉ tiêu (weight, data_type) → thang điểm | batch 2; bỏ SQL expression của EPAS |

Điểm thiết kế: các catalog đơn giản dùng chung 1 bảng-shape (cột common +
`attributes` jsonb được validate bởi registry trong `internal/service`), thêm
catalog mới = 1 entry registry + seed migration, không phải sửa repo/handler/router.
Lãi suất là domain có cấu trúc riêng (header + tiers hiệu lực).

## Quy ước

- `tenant_id IS NULL` = dữ liệu reference toàn cục, chỉ thay bằng migration;
  API chỉ ghi được row của tenant mình (bắt header `X-Tenant-Id` từ auth-gateway).
- Migration: goose, prefix `mdm_`, unique index theo `(code, COALESCE(tenant_id,''))`.
- Không có gRPC — chỉ HTTP qua auth-gateway BFF; policy: `mdm.read` / `mdm.manage`
  (đăng ký ở `apps/auth-gateway/configs/policy.yaml` + iam permissions migration).

## Run

```bash
export DATABASE_DSN="$(cat /path/to/mdm_dsn)"   # DSN luôn cấp qua env/secret, không hardcode
go run ./cmd/mdm-service     # :8096
go test ./...
```

DB provision bằng `arda-infra/k8s/bootstrap-app-databases.sh` (role `arda_mdm`, DB `mdm`).
