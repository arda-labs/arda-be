# loan-service

Domain tín dụng của Arda (P1) — đối ứng `be_loan` của EPAS (57 controller,
21 BPMN). Data model tham chiếu schema `dev_lnm` thật (dump 2026-09-06, xem
`docs/epas-survey/`); các bảng snapshot `_a`/`_h` của EPAS được bỏ chủ ý —
số dư nằm trên agreement và cập nhật transactional.

## Domain

| Khối | Endpoint | Nội dung |
|---|---|---|
| Contracts | `/api/loan/contracts` (+`/{id}`, `/{id}/submit`) | Hợp đồng tín dụng; submit → case `LOAN_FORMATION_V2` đa cấp (LNM.201.01) |
| Agreements | `/api/loan/agreements` | Khoản giải ngân theo hợp đồng (đối ứng `lnm_inf_agreement`) |
| Repay plans | `/api/loan/repay-plans` | Lịch trả nợ gốc/lãi |
| Mortgages / Collaterals | `/api/loan/mortgages`, `/api/loan/collaterals`, `/api/loan/contract-collaterals` | Thế chấp + TSĐB + gán vào HĐ |
| Adjustments | `/api/loan/adjustments/{kind}` (+`/{id}`, `/{id}/submit`) | 10 flow duyệt: debt-change, rate-change, restructure, waiver, writeoff, recovery, fund-check, revenue-allocation, vfu-fee-allocation, off-balance-export |

Điểm thiết kế: các flow duyệt dùng chung một shape (`Adjustment`) — một bảng
mỗi kind, payload jsonb được validate theo kind ở service layer; thêm flow mới
= 1 entry registry + 1 bảng + BPMN seed, không sửa repo/handler/router.

## Luồng duyệt

- Service submit case qua gRPC `WorkflowCommandService` (boundary: chỉ
  workflow-service nối Zeebe).
- Workers `lnm.<kind>.validate|execute|cancel` (workflow-service) gọi ngược
  `LoanCommandService` (gRPC mTLS) — CheckAdjustment/ResolveAdjustment.
- Resolve APPROVE của debt-change/rate-change/recovery tự áp side-effect lên
  agreement (đổi nhóm nợ / đổi lãi suất / giảm dư nợ).

## Run

```bash
export DATABASE_DSN="$(cat /path/to/loan_dsn)"   # DB `loan`, role `arda_loan`
export WORKFLOW_GRPC_ADDR="workflow-service:9090"
go run ./cmd/loan-service    # HTTP :8097 (container: 8080), gRPC :9090
go test ./...
```
