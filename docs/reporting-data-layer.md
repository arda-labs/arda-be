# Reporting data layer (P3 — QCMS/KPI)

> Trạng thái: **Bước 1 đã code** (2026-09-22) — pilot loan + deposit
> (`/internal/reporting/*` + 2 fact table + job `RPT_EXTRACT_DAILY` + 2 builder
> đọc fact). Chờ deploy (goose apply trên cluster) để chạy end-to-end.
> Phạm vi: `arda-be` (statistical-service + finance-service + domain services).
> Mục tiêu: nền dữ liệu cho **toàn bộ 923 chỉ tiêu** QCMS/QTDND (xem
> `PCF-10-KPIs.xlsx`; bản máy đọc:
> `docs/epas-survey/exports/pcf-kpi-catalog.json`), phục vụ báo cáo, KPI, biểu
> đồ, xuất tài liệu, và các nhóm SOCIUS 1–3 + 7.
> Liên quan: `docs/epas-survey/fac-statistical-reporting-survey.md` (khảo sát
> EPAS), `docs/service-boundary-design.md` (Q8), `docs/epas-to-arda-matrix.md`
> (§P2 QCMS), `arda-be/docs/ai/` (Olorin).

## 1. Vấn đề

`statistical-service` đã có khung report (11 builder tham số hoá + run/export
XLSX + submissions) nhưng **chưa chạy ra số**: các builder truy vấn
`lnm_agreements`, `dpm_savings`, `cfc_contracts`, `cfc_movements`,
`lnm_collaterals`, `customers` theo tên, trong khi DB của statistical-service
**không chứa** các bảng đó. Chính migration ghi rõ đây là điều kiện tiên quyết:

> "the statistical database must expose those read-only views (**ETL step**)
> before the reports run" — `apps/statistical-service/migrations/20260911050000_rpt_report_seeds.sql:6`

Trong toàn `arda-be` không có `CREATE VIEW` / FDW / ETL nào. Ngoài ra báo cáo
theo kỳ ("cuối kỳ", "T vs T-1", "trung bình 3 tháng") cần dữ liệu **as-of** mà
loan/deposit/capital hiện không có.

## 2. Bằng chứng EPAS (khảo sát + probe DB dev)

### 2.1 Hai cơ chế, không phải một

| | FAC (báo cáo tài chính) | Statistical/rpt (QCMS) |
|---|---|---|
| Schema | cùng `dev_fac`, đọc tại chỗ | schema riêng `dev_rpt` |
| Compute | SQL proc trên `fac_inf_trial_balance` | Oracle SP ghi vào **dump table** theo sheet |
| Nạp dữ liệu | **1 precompute** `proc_fac_aggregate_trial_balance` từ bảng txn thô | **ETL/staging**: business data phải import/stage trước |
| Config | code + proc + Jasper | `CTG_CFG_*`, SQL-as-config |

Kết luận: nửa kế toán = precompute cùng DB; nửa thống kê = **schema riêng +
staging/ETL**. Arda đã mirror đúng nửa kế toán (`fin_trial_balance_daily` +
`fin_statement_formula`), còn nửa thống kê chính là việc của tài liệu này.

### 2.2 Mô hình snapshot của EPAS (probe `10.10.0.31:31100`, Postgres 15)

EPAS dùng nhất quán 4 loại bảng:

| Hậu tố | Ý nghĩa | Ví dụ (rows) |
|---|---|---|
| `*_inf_*` | bảng kết quả cuối (current) | `lnm_inf_agreement` (16.360) |
| `*_a` | snapshot theo giao dịch (`data_time`, `txn_date`) | `lnm_inf_agreement_a` (31.195) |
| `*_h` | **snapshot theo ngày** (`data_date`) | `lnm_inf_agreement_h` (30.475) |
| `*_txn_*` | dữ liệu đang trong luồng xử lý | `lnm_txn_vfu_*` |

Đặc điểm quan trọng:
- **`org_code` có mặt trên mọi bảng `_inf`** (contract, agreement, coll,
  repay_plan, trial_balance, acc_bal, member, cust, dpm, cfm, ibm) — đây là
  khoá phân quyền + chiều phân tích.
- `fac_inf_trial_balance`: `data_date, org_code, currency_type, acc_coa_code,
  acc_scope, is_input_locked, period_acct, opn_dr_bal, opn_cr_bal,
  incr_dr_bal, incr_cr_bal, clo_dr_bal, clo_cr_bal` (14.500 rows).
- `crm_inf_member`: `customer_code, member_book_no, member_type_code,
  open_date, estb_capital_amt, add_capital_amt, total_capital_amt,
  member_status` — **domain "thành viên/vốn góp" mà Arda chưa có**.
- `crm_inf_cust`: có `org_code, customer_type, economic_type_code,
  industry_code, province_code, ward_code, area_code, is_insurance` — đủ cho
  19 chiều phân tích.
- `ibm_inf_brw_contract` / `ibm_inf_dep_contract` — hợp đồng vay/tiền gửi TCTD
  (Arda chỉ có `ibm_products`/`ibm_movements`).
- `ctg_cfg_stat_kpi` trong dev **rỗng** → catalog 923 chỉ tiêu nằm ở Oracle
  prod + file `PCF-10-KPIs.xlsx`.

## 3. Quyết định: Option B — ETL read model

**Chọn:** dựng **fact/dim read model** trong DB `statistical` bằng **job ETL**
chạy sau EOD, đọc domain service qua endpoint nội bộ có ký; tái dùng
`fin_trial_balance_daily` cho nhóm kế toán.

Lý do:
- Giữ **cô lập DB-per-service** (EPAS cũng không đọc chéo DB).
- Cho phép **snapshot theo `business_date`** → giải luôn bài toán as-of và
  chỉ tiêu C (tăng trưởng/trung bình/tỷ lệ).
- Có precedent trong Arda: `fin_trial_balance_daily` + job
  `/internal/jobs/trial-balance-daily` + EOD step `FIN_TRIAL_BALANCE_DAILY`.
- Arda đã tự chốt: *"arda loads data via ETL jobs, not Excel uploads"*
  (survey §5.2 mục 5).

**Loại bỏ:**
- FDW/`postgres_fdw` view trong statistical DB: phá cô lập, credential chéo DB.
- Job runner đọc thẳng DSN domain: statistical-service giữ nhiều DSN (mùi bảo
  mật), enforce scope nằm trong SQL.

## 4. Kiến trúc đích

```
domain services (data owner)                statistical-service
┌───────────────┐  signed GET              ┌──────────────────────────────┐
│ loan          │ /internal/reporting/...   │ ETL job (RPT_EXTRACT_DAILY)  │
│ deposit       │ ────────────────────────► │  ├─ rpt_dim_*                │
│ capital       │                           │  └─ rpt_fact_*_daily         │
│ finance       │ (trial balance nội bộ)    │                              │
│ crm / ibm     │                           │ indicator engine (P/C)       │
└───────────────┘                           │ report instance + audit      │
        ▲                                   │ render: excelize / FE grid   │
        │ EOD COB (platform plt_job_*)      │ export: media-service        │
        └───────────────────────────────────└──────────────────────────────┘
```

Nguyên tắc:
- **Compute = Go (builder/engine), definition = config table, không SQL thô**
  (Q8). Formula dạng JSON tham chiếu fact/dim/indicator khác — theo pattern
  `fin_statement_formula`.
- **Một lớp reporting**, không tái tạo "hai stack không gặp nhau" của EPAS.
  Finance giữ vai data owner (trial balance + statements); statistical tiêu thụ
  lại + phục vụ regulatory returns.

## 5. Read model schema (đề xuất)

### 5.1 Dimensions

| Bảng | Cột chính | Nguồn |
|---|---|---|
| `rpt_dim_org` | tenant_id, org_code, parent_org_code, org_name, branch_code, region_code, area_code | platform/iam |
| `rpt_dim_employee` | tenant_id, employee_code, org_code, position_code | hrm |
| `rpt_dim_product` | tenant_id, domain, product_code, product_name, term_months, product_group | loan/deposit/capital |
| `rpt_dim_customer` | tenant_id, customer_code, customer_type, economic_type_code, industry_code, province_code, ward_code, area_code, is_member | crm |

### 5.2 Facts (grain: tenant × business_date × org × entity)

| Bảng | Grain | Cột đo lường chính | Mirror EPAS |
|---|---|---|---|
| `rpt_fact_loan_agreement_daily` | tenant, business_date, org_code, agreement_code | disburse_amt_minor, outstanding_minor, debt_group_code, provision_minor, acr_interest_minor, off_bal_principal_minor, off_bal_interest_minor, overdue_date, maturity_date | `lnm_inf_agreement_h` |
| `rpt_fact_loan_contract_daily` | tenant, business_date, org_code, contract_code | loan_amt_minor, first_disburse_minor, purpose_code, industry_code | `lnm_inf_contract_h` |
| `rpt_fact_deposit_contract_daily` | tenant, business_date, org_code, dpm_contract_code | principal_minor, acr_interest_minor, term_value, term_unit, open_date, maturity_date, closed_date | `dpm_inf_contract_h` |
| `rpt_fact_capital_contract_daily` | tenant, business_date, org_code, cfm_contract_code | total_committed_minor, total_receipt_minor, fund_type_code, fund_purpose_code, maturity_date | `cfm_inf_contract` |
| `rpt_fact_collateral_daily` | tenant, business_date, org_code, coll_code | coll_value_minor, coll_use_value_minor, coll_type_code, valuation_date | `lnm_inf_coll_h` |
| `rpt_fact_member` | tenant, business_date, org_code, customer_code | estb_capital_minor, add_capital_minor, total_capital_minor, member_type_code, member_status | `crm_inf_member` |
| `rpt_fact_ibm_deposit_daily` | tenant, business_date, org_code, dep_contract_code | principal_minor, acr_interest_minor, credit_inst_code, term_value | `ibm_inf_dep_contract` |
| `rpt_fact_ibm_borrow_daily` | tenant, business_date, org_code, brw_contract_code | brw_amt_minor, credit_inst_code, brw_purpose_code, maturity_date | `ibm_inf_brw_contract` |
| `rpt_fact_gl_daily` | tenant, business_date, org_code, account_code, currency | open/incr/close Dr/Cr, acc_scope, period_acct | `fac_inf_trial_balance` |

Nhóm kế toán **không tạo fact mới** — bổ sung chiều `org_code` (+ `acc_scope`,
`period_acct`) vào `fin_trial_balance_daily` và cho statistical đọc bản mirror.

### 5.3 Indicator model

Mở rộng `rpt_indicators` (hiện chỉ code/name/unit/group):

| Cột mới | Ý nghĩa |
|---|---|
| `kpi_type` | `P` (primary, lấy từ fact) / `C` (calculated) |
| `periodicity` | D/W/M/Q/Y (bitmask hoặc text) |
| `formula` JSONB | tham chiếu fact/dim/indicator khác; KHÔNG chứa SQL thô |
| `dimensions` JSONB | chiều phân tích hợp lệ (thời gian, org, kỳ hạn…) |
| `unit`, `rounding`, `display_format` | định dạng hiển thị |
| `score_group`, `weight` | phục vụ GSATHĐ chấm điểm (nhóm + tổng) |

Kết quả: `rpt_indicator_results` (tenant, instance_id, kpi_code, period_code,
dimension_key, value, rev_no) + `rpt_indicator_audit` (cell-level, học từ EPAS
`RPT_TXN_STAT_KPI_AUDIT`).

### 5.4 Report instance + governance

Học EPAS `RPT_TXN_STAT_TEMPLATE`:
`rpt_report_instances` (template × org × period, revision, agg_run_no, status,
workflow_case_id, SLA) — governance qua **workflow-service case-types**, không
tự dựng config workflow engine.

## 6. ETL job

- Job `RPT_EXTRACT_DAILY` khai báo trong `plt_job_definitions` (EOD COB,
  platform-service), chạy sau `FIN_TRIAL_BALANCE_DAILY`.
- Idempotent theo `(tenant_id, business_date)`; checkpoint trong
  `plt_job_runs.checkpoint`.
- Readiness check trước khi ghi fact (học EPAS `SOURCE_CHECK_SQL`).
- Nguồn extract: endpoint `/internal/reporting/*` (signed workload identity,
  cùng cơ chế `internalAIService` nhưng **allowlist cột đầy đủ** — khác
  `/internal/ai/*` vốn đã redacted cho chat).
- Snapshot: mỗi lần chạy ghi một `business_date`; backfill được theo ngày.

## 7. Bao phủ 923 chỉ tiêu (ước lượng)

| Chủ đề (Summary) | SL | Arda | Trạng thái |
|---|---:|---|---|
| Tài chính kế toán | 307 | finance `fin_trial_balance_daily` | ✅ có nền, thiếu chiều org |
| Tín dụng | 251 | loan | ✅ có bảng, thiếu fact + as-of |
| GSATHĐ (an toàn) | 178 | dẫn xuất finance+loan+deposit | ⚠️ cần rule/score engine |
| Huy động vốn | 60 | deposit | ✅ có bảng, thiếu fact |
| Góp vốn cổ phần | 54 | — | ❌ thiếu domain thành viên |
| Khách hàng | 36 | crm customers | ⚠️ thiếu dim + `is_member` |
| Tiền vay TCTD | 26 | — | ❌ thiếu (IBM borrow) |
| Tiền gửi TCTD | 11 | deposit ibm (một phần) | ⚠️ thiếu hợp đồng |
| **Tổng** | **923** | | ~654 nền có, ~80 thiếu domain, ~178 dẫn xuất |

Các chủ đề DanhMuc chưa có sheet trong file (Kho quỹ, Chuyển tiền, Nhân sự
tiền lương, TSCĐ, CCDC) chưa tính.

## 8. Lộ trình

1. **Bước 1** — Read model pilot: `/internal/reporting/*` cho loan + deposit;
   `rpt_fact_loan_agreement_daily` + `rpt_fact_deposit_contract_daily`; job
   `RPT_EXTRACT_DAILY`; sửa 2 builder (`loan_portfolio_summary`,
   `deposit_portfolio`) đọc fact; RunReport/Export ra số thật.
2. **Bước 2** — As-of/snapshot: **đã code** — as-of = `max(business_date)`
   trong builder; `fin_trial_balance_daily` thêm `org_code` (PK mở rộng) +
   `PostingService` stamp org vào entry metadata + `RebuildDaily`/`ListDaily`
   dùng org. Chưa chạy trên cluster.
3. **Bước 3** — Indicator model: **đã code** — `rpt_indicators` mở rộng
   (P/C, periodicity, `formula` JSON khai báo, dimensions, score) +
   `rpt_indicator_results` + `rpt_indicator_audit`; API
   `/api/statistical/indicator-results` + compute engine
   (`POST /api/statistical/indicators/compute`, whitelist đóng) + compute series
   (growth `previous_period`/`same_period_last_year`, `trailing_average`);
   seed 13 chỉ tiêu mẫu.
4. **Bước 4 — Presentation — ĐÃ CODE** (commit `1c7f3e9f` + infra `7a33c27`):
   `internal/presentation` (chart picker deterministic + HTML print template +
   XLSX có title/KPI/table); `presentation_service` dựng document từ report
   result + indicator đã lưu (document và API không thể lệch nhau); PDF qua
   arda-doc Gotenberg, thiếu `GOTENBERG_URL` → 503 rõ ràng thay vì file rỗng.
   Route: `GET /reports/{code}/chart`, `/reports/{code}/document?format=`,
   `/indicators/document?period_code=`. Fix tên file lặp period.
   Còn lại: verify trên cluster.
5. **Bước 5** — NL routing: hỏi tự nhiên → chọn report/indicator + tham số
   (không sinh SQL), qua ai-service.
6. **Bước 6** — Domain thiếu (trần 923 chỉ tiêu): `member`/vốn góp cổ phần
   (54) + IBM borrow/deposit (37) + `CT_GIAO_DICH`; chỉ tiêu C (tăng trưởng/
   trung bình 3 tháng) cần thêm bảng kết quả đã tính.
7. **Bước 7** — Proactive (SOCIUS nhóm 7): rule + as-of + notification-service.

Ánh xạ SOCIUS: nhóm 1–3 + 7 dùng chung Bước 1–5; nhóm 4 (document-to-txn) và
5 (instruction-to-txn) thuộc P3 riêng (media + HITL); nhóm 6 (system operation)
**ngoài phạm vi**.

## 9. Quyết định mở

- Snapshot: ghi fact theo ngày cho **mọi** bảng hay chỉ bảng cần so kỳ?
- Có bổ sung domain **thành viên/vốn góp cổ phần** (54 chỉ tiêu) và **IBM
  borrow/deposit** (37 chỉ tiêu) không?
- Backfill lịch sử: từ EPAS `_h` (nếu migrate) hay chỉ từ ngày bật ETL?
- Chiều phân tích: 19 chiều của EPAS — làm dim đầy đủ hay theo nhu cầu?

## 10. Non-goals

- Không SQL-as-config (`EXPRESSION_SQL`), không free-form text-to-SQL.
- Không FDW, không cho statistical-service giữ DSN domain.
- Không port Aspose/Jasper; render bằng excelize + Gotenberg + FE grid.
- Không import Excel thủ công làm đường nạp chính.

## Phụ lục A — Nguồn dữ liệu 923 chỉ tiêu (từ `PCF-10-KPIs.xlsx`)

| Prefix EPAS | Ý nghĩa | Arda tương ứng |
|---|---|---|
| `DC_*` | danh mục (khách hàng, thành viên, khu vực) | crm/mdm |
| `BL_*` | tiền gửi (contract, account) | deposit |
| `TD_*` | tín dụng (hợp đồng, khế ước) | loan |
| `KT_*` | kế toán (tài khoản, tổng hợp, phát sinh) | finance |
| `TT_*` | tiền gửi/vay TCTD | ibm (thiếu) |
| `BCTK_*` | báo cáo tài chính | finance statements |
| `NSU_*`, `TMP_DC_NHAN_VIEN` | nhân sự | hrm |
| `CT_GIAO_DICH` | chuyển tiền | — (chưa rõ) |

## Phụ lục B — Cách probe EPAS dev (không kèm credential)

DB dev EPAS: Postgres 15 tại `10.10.0.31:31100` (database `postgres`), tới được
từ workstation dev. Không có `psql`; dùng `pg8000` (pure Python) qua `uv`:

```powershell
$uv = Join-Path $env:USERPROFILE '.local\bin\uv.exe'
# script đọc schema/row-count; DSN lấy từ cấu hình EPAS (không commit vào Arda)
& $uv run --python 3.12 --no-project --quiet --with pg8000 <script.py>
```

Schemas: `dev_lnm`, `dev_dpm`, `dev_cfm`, `dev_ibm`, `dev_fac`, `dev_crm`,
`dev_rpt`, `dev_vcm`, `dev_hrm`, `dev_cmn`, `dev_dgm`, `dev_bpm`, `dev_edu`,
`dev_mobile`, `mob_*`. Lưu ý: bảng nguồn KPI (`DC_/BL_/TD_/KT_/TT_`) **không**
nằm ở Postgres dev — chúng thuộc Oracle core (prod); `dev_rpt.ctg_cfg_stat_kpi`
rỗng ở dev. Dùng read-only, không dump dữ liệu khách hàng (PII).
