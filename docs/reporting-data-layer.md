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
5. **Bước 5 — NL routing — ĐÃ CODE** (commit `1f10db81`): 2 tool đọc trên AI
   surface — `arda.statistical.runReport` (`GET /internal/ai/report-run`: chọn
   báo cáo theo `report_code` + `period_code`, trả rows đã tính; query_id/SQL/
   param schema không rời service) và `arda.statistical.listIndicatorResults`
   (`GET /internal/ai/indicator-results`: giá trị chỉ tiêu đã lưu + tên/đơn vị;
   formula/sources/dimensions không lộ). Catalog regen 33 entry; dùng lại
   `statistical.read` + policy wildcard sẵn có. **Đây là routing, không phải
   text-to-SQL** — model chỉ được nêu tên báo cáo/chỉ tiêu có thật (đúng Q8).
6. **Bước 6 — Domain thiếu — ĐÃ CODE (member domain đầy đủ)**: `crm-service`
   có domain thành viên QTDND — `crm_member_products`/`crm_members`/
   `crm_member_requests`; register + góp/rút vốn maker-checker qua case
   `CRM_MEMBER_V1` (BPMN `crm-member-v1` + roles CRM_MAKER/CHECKER + SLA);
   decision ghi ngược qua proto `MemberCommandService`; FE `/customers/members`
   (list + dialog góp/rút); reporting: 2 fact table + ETL + 12 chỉ tiêu seed;
   IAM `crm.member.read/manage` + policy route. Commit BE `58472a8a`, FE `769b28a`.
   Còn lại Bước 6: IBM borrow/deposit (37 chỉ tiêu) + kho quỹ/chuyển tiền/TSCĐ.
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

## 9b. Bài học verify trên cluster (2026-09-22)

Chạy thật phát hiện 5 lỗi mà unit test không thấy:

1. **`rpt_indicator_results.id` là UUID** nhưng code truyền `indres_<hex>`
   (`NewStatisticalID`) → `SQLSTATE 22P02`, chặn **mọi** compute. Bài học: bảng
   dùng `DEFAULT uuidv7()` thì để DB sinh id.
2. **EOD seed `loan-service:8097`** trong khi Service thật là 8080 → 2 step loan
   fail timeout mỗi lần COB. Endpoint job phải khớp Service port thật.
3. **Tên file tải về lặp period** (`indicators-2026-09-2026-09.pdf`) khi tên đã
   chứa period.
4. **`coerceArg` chỉ nhận `float64`** — Goja truyền số nguyên JS sang Go dạng
   `int64`, nên `limit: 20` (giá trị đúng) bị từ chối `must be a number` và
   **mọi tool phân trang** fail. Bind tham số phải chấp nhận mọi kiểu số; nhúng
   ràng buộc vào JSDoc (`[1..20, default 10]`) để model thấy cạnh tên tham số.
5. **Chỉ tiêu amount bị nhân 100** (`"scale":100`) trong khi report đọc cùng cột
   trả giá trị gốc → chỉ tiêu 10 tỷ vs sổ thật 100 triệu. Arda lưu tiền là
   `int64 *_minor` theo ISO 4217 (`docs/db-schema-conventions.md` §5); **VND có
   0 chữ số thập phân nên minor == major** → không chỉ tiêu VND nào được có
   scale. Model tự phát hiện mâu thuẫn khi trả lời.

Hệ quả thiết kế: chỉ tiêu và report **phải đọc cùng một fact column với cùng
đơn vị**; mọi khác biệt đơn vị phải là bước biến đổi tường minh có test.

3 bug còn lại (6–9) đến từ **migration/khởi động**, và cùng một nguyên nhân gốc:
**bắt chước migration cũ mà không đọc schema hiện tại**. Migration cũ chỉ chứng
minh nó *từng* hợp lệ; schema đã tiến hoá sau đó.

6. **gRPC service đăng ký sau `Serve()`** (`crm-service` crash-loop:
   `Server.RegisterService after Server.Serve`). Mọi `RegisterXxxServiceServer`
   phải chạy trước `grpcSrv.Serve(...)`; service phụ thuộc workflow client nên
   dial phải nằm trên khối gRPC.
7. **FK `workflow_assignment_rules.role_code` → `workflow_role_catalog`**:
   phải seed catalog role *trước* assignment rules (thứ tự, không phải di tích).
8. **`ON CONFLICT (code)` trên `iam_roles`** → `SQLSTATE 42P10`: unique hiện tại
   là `(tenant_id, code)`; seed cũ ghi `(code)` chỉ vì nó chạy trước khi đổi.
9. **`iam_roles.tenant_id = 'default'`** → `SQLSTATE 23514`
   (`iam_roles_explicit_tenant_ck` cấm `''`/`'default'`); roles thật dùng tenant
   pilot (`…010`).

Quy tắc rút ra: trước khi thêm migration/khởi động, kiểm tra **constraint,
unique index và thứ tự phụ thuộc hiện hành**, không suy ra từ file cũ.

## 9c. Bài học verify HTTP write path (2026-09-22)
Hai bug nữa chỉ lộ khi **gọi thẳng endpoint như FE gọi** (unit test dựng struct
trong Go nên không chạm đường decode/serialize):

10. **Request struct thiếu `json:` tag** (`RegisterMemberInput`,
    `SubmitCapitalRequestInput`). `encoding/json` không bind được payload
    snake_case của FE → handler decode ra struct rỗng → mọi register/request
    fail `"customer_code is required"` **trước khi tới service**. Unit test và
    gRPC callback đều dựng struct bằng Go nên xanh.
    - `check-json-tags.mjs` cũ chỉ chặn camelCase, **không** đòi có tag. Đã
      thêm invariant: struct là đích của `json.NewDecoder(r.Body).Decode(&x)`
      phải có ≥1 json tag (resolve `x` về `var x T` gần nhất; tên type không
      unique — `iam`/`platform` cùng có `Organization`, chỉ 1 có tag — nên chỉ
      fail khi **mọi** struct cùng tên đều 0 tag).
11. **`$8 + $9` với tham số untyped** trong INSERT `crm_members` →
    `SQLSTATE 42725 "operator is not unique: unknown + unknown"`. Mọi register
    fail ở INSERT dù body đã decode đúng. Sửa: `$8::bigint + $9::bigint`.
12. **`CreateCase` thiếu `TenantID`** (và `CaseCode`/`PrimaryObjectType/ID`/
    `DomainService`) → workflow-service trả `InvalidArgument: tenantId is
    required`; mọi yêu cầu góp vốn fail 400 **sau khi** DRAFT đã persist (bản
    ghi mồ côi). Sửa: dựng `CaseCreate` như luồng đăng ký khách hàng đang chạy.
13. **Checker token version lệch một nhịp**: `SubmitCase` chạy **trước**
    `MarkMemberRequestSubmitted` (thao tác này mới tăng `version`), nên case
    variable `dataVersion` luôn nhỏ hơn version thật của request. `ST_Execute`
    retry vô hạn với `member changed while you were editing`; vốn góp không bao
    giờ chuyển. Sửa: mark SUBMITTED trước rồi mới submit case với version
    **sau** khi tăng; thêm `orgId`/`actorUserId` — đúng key `crmJobContext` đọc.

Ghi nhận thêm (chưa sửa, không thuộc member):
- **Workflow user-task projector**: `case projection: upsert user task work item`
  fail `SQLSTATE 42804 could not determine polymorphic type because input has
  type unknown`, retry mỗi 2s → work item của checker không được project.
- **Timeline completion** log WARN `SQLSTATE 42P08 inconsistent types deduced for
  parameter $2` — sự kiện timeline bị mất im lặng ở **mọi** completion, không
  riêng member.
- **`$3 + $4` untyped trong `ApplyApprovedCapital`** (bug 11 nay lộ mặt thứ hai):
  `SQLSTATE 42725 operator is not unique: unknown + unknown`. Tưởng int64 Go local
  là an toàn, nhưng pgx vẫn gửi `unknown` trong câu UPDATE này — đã cast bigint.
14. **Key fact request không duy nhất**: `memberRequestKey` =
    `member_code:request_type:request_date`. Một thành viên góp vốn bổ sung
    **nhiều lần trong cùng ngày** → trùng key → `rpt_fact_member_request_daily_pkey`
    `SQLSTATE 23505` → cả bước `RPT_EXTRACT_DAILY` fail, **không fact nào được ghi**
    (kể cả member fact). Sửa: mang `request_id` từ CRM reporting surface qua và
    dùng làm key.

Quy tắc rút ra: khoá fact phải là **định danh nguồn** (id), không phải tổ hợp
thuộc tính "chắc là đủ unique"; và một bước ETL fail phải nhìn ra nó chặn **toàn
bộ** fact trong cùng transaction.
15. **So sánh filter trên cột số bị so như text**: engine bind literal dạng text
    rồi so nguyên cột → `bigint > text` (`SQLSTATE 42883`); chỉ tiêu
    `10022.01` (vốn góp > 0) fail. So text cũng **sai số học** (`"9" > "10"`).
    Sửa: whitelist cột số (`numericFactColumns`) → cast `col::numeric op
    ALL($n::numeric[])`.
16. **Seed chỉ tiêu series sai dạng**: `growth`/`trailing_average` phải trỏ
    **mã chỉ tiêu cơ sở** (`indicator`) vì `ComputeSeries` đọc **giá trị đã lưu**
    qua các kỳ, không đọc fact. Seed dùng `fact`+`column` → `growth` fail rõ,
    còn `trailing_average` **im lặng trả `null`** (lỗi tệ hơn vì không ai biết).
    Sửa seed gốc + migration vá môi trường đã chạy.
17. **Dimension/filter bằng trên cột số**: `term_months = $n::text` sinh
    `integer = text`. Sửa: helper `eqExpr` cast cột số sang text cho predicate
    **bằng** (an toàn — equality không đổi ngữ nghĩa), giữ `::numeric` cho so
    sánh thứ tự.

## 9e. Bước 6b-1 (tiền gửi TCTD) — ĐÓNG, verified trên cluster

Fact `rpt_fact_ibm_deposit_daily` + surface `/internal/reporting/ibm-deposits`
(deposit-service) + ETL + **11 chỉ tiêu** PCF "Tiền gửi TCTD".

Bằng chứng:
- Fact ghi đúng: `IBM-T-0001` term 3 (từ product), 200tr, 4.5%; `IBM-T-0002`
  term 0, 100tr, 0.5%.
- Compute `2026-09` → **35/35 computed, 0 failed**.
- Số khớp fact: 60000.01 = 300tr · 60002.02 = 200tr · 60002.03 = 100tr ·
  60002.04 = 3,5tr · 60004.01 = 2 · 60004.37 = 2.5.
- Series thật: base 2026-08 = 200tr → 60000.01.01 = **250tr** (TB 3 tháng),
  60000.01.02 = **0.5** (+50%).

**Cố ý chưa làm**: chia NHHT / NHNN / TCTD khác — cần registry
`platform.plt_credit_institutions` (đang rỗng). Hardcode mã "NHHT" sẽ đặt **số
sai lên báo cáo quy định**. Fact giữ `counterparty_code` + dimension
`counterparty` nên chỉ cần thêm filter khi registry có dữ liệu.

Còn lại của Bước 6: **tiền vay TCTD (26 chỉ tiêu)** — chưa có domain trong Arda
(cần bảng `ibm_borrows` + workflow); kho quỹ/chuyển tiền/TSCĐ. Sau đó Bước 7.

## 9f. Bước 6c (Tài chính kế toán, 322 chỉ tiêu) — nền dữ liệu đã xong

Nhóm "Tài chính kế toán" (227 P + 80 C + 15 khác) **không** lấy từ bảng nghiệp vụ
riêng mà từ **tổng hợp kế toán** (`KT_TONG_HOP`): mỗi chỉ tiêu là một công thức
tài khoản, ví dụ `DCN TK 10` (tiền mặt), `DCN TK 21 - DCC TK 21` (cho vay thuần).

Phân loại công thức trong catalog (227 chỉ tiêu P):
- 112 dạng đơn giản `DCN/DCC TK <n>` (67 + 45)
- 44 dạng biểu thức (cộng/trừ/điều kiện)
- 71 dạng khác (`Dư nợ/Dư có TK`, `DCC các TK: …`, `Tự tính = …`, hằng số)

Nền dữ liệu: `rpt_fact_trial_balance_daily` (statistical) ←
`GET /internal/reporting/trial-balance` (finance-service) ← `fin_trial_balance_daily`.
Grain tenant × date × account_code × currency × org; giữ `account_code` nguyên
bản (prefix là khoá map) và giữ cả `close_debit_minor`/`close_credit_minor`
(vài chỉ tiêu đọc thẳng bên Có).

Chưa seed chỉ tiêu: cần **parser** đọc công thức + **cơ chế đối soát** để không
đặt số sai lên báo cáo tài chính.

### Parser — đã có, phủ 155/184 công thức thật

`ParseAccountFormula` (statistical-service `internal/indicator`) dịch công thức
PCF thành aggregate `account_balance` gồm các **số hạng có dấu**:

```json
{"type":"account_balance","accounts":{"fact":"rpt_fact_trial_balance_daily",
 "terms":[{"side":"debit","prefixes":["30"]},
          {"side":"credit","prefixes":["305"],"sign":-1}]}}
```

`side` = `debit` (DCN/Dư nợ) · `credit` (DCC/Dư có) · `net` (DN−DC). Số hạng có
`sign` (-1 khi bị trừ), `exclude` (`36 (trừ 366)`), và `clamp` (`nếu …>0`).

An toàn:
- Prefix tài khoản phải khớp `^[0-9]{1,6}$` và **bind tham số**, không nối SQL.
- Whitelist đóng: fact/column phải có trong `factColumns`/`numericFactColumns`.
- Công thức mô tả (`Tự tính = …`, `Trùng công thức …`, `=0`, `PSN TK …`) **bị từ
  chối** — không đoán số cho báo cáo quy định.
- Điều kiện `nếu …` chỉ nhận khi biểu thức là **một nhóm duy nhất**; dạng
  `A + B nếu X>0` mơ hồ nên từ chối.
- Range `TK 313004 đến 313011` chỉ mở tối đa 100 prefix.

Fixture `testdata/account_formulas.json` (184 công thức P thật) + test coverage
giữ sàn 100; hiện **155/184 parse được, 29 từ chối**. Trong 29 đó: ~10 là mô tả,
~10 là chỉ tiêu **tỷ lệ** (cần `type: ratio` tham chiếu chỉ tiêu khác, không phải
`account_balance`), phần còn lại là biểu thức điều kiện nhiều nhánh.

### Seed + đối soát — đã có, NHƯNG seed bị chặn bởi hệ tài khoản

- **Parser + `account_balance`** đã xong (155/184 công thức).
- **`ReconcileAccounting`** đã xong: trial balance phải cân, và mỗi công thức được
  **tính lại bằng SQL tay** rồi so với engine. Đường tính tay **từ chối** block nó
  không mô phỏng được (term có clamp) thay vì so hai công thức khác nhau.
- **Seed: CHƯA bật** — xem blocker dưới.

#### Blocker: hai hệ tài khoản khác nhau (TT31 vs TT92)

Công thức PCF viết theo **hệ tài khoản QTDND Thông tư 31** (`TK 10` tiền mặt,
`TK 13` tiền gửi TCTD, `TK 21` cho vay, `TK 30`/`305` TSCĐ, `41512` vay TCTD…).

Nhưng sổ pilot của Arda hạch toán theo **Thông tư 92** (`fin_coa_accounts` V1, 40
tài khoản): `1011` Tiền mặt, `1131` Tiền gửi ngân hàng, `1311` Cho vay khách
hàng, `1321` Tiền gửi tại TCTD khác, `1319` Dự phòng phải thu khó đòi…

Khớp theo **prefix** giữa hai hệ là **sai âm thầm**:

| PCF | Prefix khớp | Khớp nhầm vào |
|---|---|---|
| `TK 13` Tiền gửi tại TCTD khác | `1311`, `1319`, `13101`, `1321` | **Cho vay khách hàng**, dự phòng… |
| `TK 21` Cho vay khách hàng | (không có `21…`) | ra 0 dù `1311` chính là cho vay |
| `TK 30` TSCĐ | (không có `30…`) | ra 0 |
| `41512` Vay TCTD | (không có) | ra 0 |

Vì vậy seed đã được **rút khỏi repo** (`20260922210000_rpt_accounting_indicator_seeds.sql`)
để không đặt số sai lên báo cáo tài chính. `TestGenerateAccountingSeed` vẫn còn
(opt-in) và sẽ sinh lại được ngay khi có mapping.

**Cần**: một bảng mapping **tường minh, do kế toán duyệt** từng tham chiếu tài
khoản PCF → `fin_coa_accounts.acc_code` (không suy theo prefix), rồi mới seed.
Hoặc chọn dùng đúng hệ TT31 cho sổ QTDND.

### Còn lại của 6c

1. Chốt mapping TT31 ↔ TT92 (quyết định nghiệp vụ, cần kế toán).
2. ~10 chỉ tiêu **tỷ lệ** (Lợi nhuận thuần/tổng tài sản…) dùng `type: ratio`.
3. Biểu thức điều kiện nhiều nhánh còn lại.

## 9m. Bước 7 — Proactive (rule + alert), đã code

Thay vì chờ ai đó mở báo cáo, vòng EOD **tự đánh giá** chỉ tiêu đã tính theo
ngưỡng và ghi cảnh báo.

| Thành phần | Nội dung |
|---|---|
| Bảng | `rpt_indicator_rules` + `rpt_indicator_alerts` |
| API | `GET/POST /api/statistical/indicator-rules`, `GET /api/statistical/indicator-alerts`, `POST .../{id}/ack` |
| EOD | `RPT_EVALUATE_RULES` (seq 40, chạy **cuối** để số đã chốt) |
| AI | `arda.statistical.listIndicatorAlerts` → Olorin trả lời "có cảnh báo gì" |

**Quyết định thiết kế:**

- Rule **chỉ so một giá trị đã lưu với một con số** — không mang SQL. Vì vậy một
  rule **chỉ có thể canh thứ engine đã tính được**; `UpsertRule` từ chối
  `indicator_code` không tồn tại.
- Alert **idempotent** theo `(rule, period, slice)`: chạy lại kỳ cập nhật cùng
  dòng; chỉ tiêu hồi phục thì **xoá alert cũ**. COB chạy lại hay backfill không
  spam hộp thư.
- Operator/severity là **tập đóng** (`> >= < <= = <>`, `INFO|WARN|CRITICAL`) ràng
  buộc bằng CHECK ở DB **và** validate ở service.
- Alert là **bản ghi bền**; gửi qua event-bus tới notification-service là mối nối
  còn lại — relay có thể publish từ chính bảng này mà không đổi hợp đồng.

### Gửi cảnh báo qua event bus — đã nối

| Thành phần | Nội dung |
|---|---|
| Outbox | `rpt_outbox_events` |
| Relay | `service.OutboxRelay` → NATS JetStream (`ARDA_EVENTS`, subject `arda.>`) |
| Event | `arda.statistical.indicator.breached.v1` (đã vào registry notification) |
| Env | `NATS_URL` từ `arda-app-secrets` (**optional**) |

- **Enqueue idempotent** theo `(rule, period, slice, value)`: COB chạy lại không
  gửi trùng; giá trị đổi khác thì gửi lại.
- Publish lỗi → row ở lại pending + đếm attempts, **không mất**.
- **Relay là optional theo cấu trúc**: `NewOutboxRelay` trả `nil` khi không có
  NATS → ETL và alert vẫn chạy (alert vốn đã bền trong `rpt_indicator_alerts`).

**Còn lại**: template `noti_templates` cho event mới (nội dung email/inbox do
nghiệp vụ cấu hình, không phải code).

## 9j. Nhóm Khách hàng (36 chỉ tiêu)

Chỉ tiêu khách hàng của PCF là **đếm theo quan hệ**, không phải "tất cả khách
hàng": thành viên / ngoài thành viên, đang gửi tiền, đang vay vốn. Đó là các vị
từ **xuyên fact**, nên ETL **đóng cờ** `is_member` / `has_deposit` / `has_loan`
lên `rpt_fact_customer_daily` **sau khi mọi nguồn đã vào** (một transaction).

Cờ dùng `VARCHAR(1)` `'Y'/'N'` (không phải `BOOLEAN`) vì engine so literal text —
boolean sẽ phải cast ở mọi filter.

Seed **10 chỉ tiêu**: 10001.01 (tổng), 10002.01 (thành viên), 10003.01 (ngoài
thành viên), 10006.01 (đang gửi tiền), 10007.01 (đang vay vốn) + cặp tăng trưởng.

**Đồng thời xoá một seed sai**: mã `60001.01` nằm **sai nhóm** ("Khách hàng") và
growth tham chiếu `60000.01` — chỉ tiêu **tiền gửi liên ngân hàng** — nên nó báo
tăng trưởng tiền gửi TCTD dưới tên chỉ tiêu khách hàng.

**Chưa seed**: nhóm "trong địa bàn / ngoài địa bàn" (cần vùng của khách hàng) và
"chuyển tiền" (cần domain chuyển tiền).

## 9l. Đối soát — đã nối vào vòng EOD

`ReconcileAccounting` trước đây chỉ là thư viện + test, **không ai gọi**. Nay có
hai đường vào:

| Đường | Dùng cho |
|---|---|
| `GET /api/statistical/indicators/reconcile?period_code=` | Admin đọc, xem mismatch |
| `POST /internal/jobs/reconcile-accounting` (EOD step `RPT_RECONCILE_ACCOUNTING`, sequence 38) | Tự động sau bước trích xuất fact |

**Hợp đồng trạng thái**: job trả **422** khi có mismatch **hoặc** trial balance
không cân → EOD engine đánh dấu bước **FAILED**. Đây chính là mục đích: mapping
tài khoản sai hay thiếu số hạng trong seed kế toán **lộ ra lúc COB**, trước khi
lên báo cáo, thay vì thành một bảng cân đối sai âm thầm.

Hai kiểm tra độc lập:
1. Trial balance **phải cân** (`SUM(close_debit) == SUM(close_credit)`) — kiểm
   tra fact + ETL, chạy **kể cả khi chưa seed chỉ tiêu kế toán nào**.
2. Mỗi công thức `account_balance` được **tính lại bằng SQL tay** (khác đường
   builder của engine) rồi so — lệch dấu, thiếu prefix hay clamp sai lộ ra thành
   mismatch.

Đường tính tay **từ chối** block nó không mô phỏng được (term có clamp) thay vì
so hai công thức khác nhau.

Test khoá hợp đồng: mismatch → 422 · clean → 200 và suy period từ `to_date` ·
thiếu tenant → 403.

## 9k. Phân tích GSATHĐ (178) — phần lớn bị chặn

| Nhóm | Số | Trạng thái |
|---|---|---|
| Dùng mã tài khoản (TK) | 33 | ⛔ chờ mapping COA |
| Không có công thức (metadata: địa chỉ, điện thoại, tên lãnh đạo) | 29 | Không tính được |
| "Điểm..." (scoring theo ngưỡng) | ~60 | Cần **engine scoring** mới |
| Nhân sự (CBTD, trình độ, chức vụ) | ~8 | Cần fact HR |
| Tỷ lệ cần vốn CSH/tài sản (CAR, giới hạn cho vay) | nhiều | ⛔ chờ mapping COA |

Vì vậy GSATHĐ **không phải nhóm rẻ**: phần lớn phụ thuộc mapping tài khoản — cùng
một blocker với 307 chỉ tiêu tài chính kế toán. Nhóm sinh lời tốt hơn là Khách
hàng (trên) và phần còn lại của Huy động vốn / Tín dụng theo kỳ hạn.

## 9g. Nhóm dẫn xuất Tín dụng / Huy động vốn

11 chỉ tiêu dẫn xuất dùng fact đã có (`rpt_fact_loan_agreement_daily`,
`rpt_fact_deposit_contract_daily`):

| Nhóm | Chỉ tiêu |
|---|---|
| Huy động vốn | 20000.01.01 (TB 3 tháng), 20000.01.02 (tăng trưởng), 20007.01 (số người gửi), 20007.01.02, 20019.01 (bình quân/người), 20019.01.02 |
| Tín dụng | 30000.01.02 (tăng trưởng dư nợ), 30000.01.03 (TB 3 tháng), 30020.01.01/.02 (nợ xấu), 30021.01.02 (dự phòng) |

**Cố ý chưa seed**: các chỉ tiêu chia theo **kỳ hạn / phương thức / địa bàn /
nông nghiệp** — cần cột term/method/region/sector mà fact chưa có; bịa ra sẽ đặt
số sai lên báo cáo.

Đồng thời sửa `60000.01.02`: seed trước thiếu `percent:true` nên lưu tỷ lệ thô
(0.5) trong khi đơn vị là `%` — nay lưu đúng 50.

## 9h. Chia theo kỳ hạn (Tín dụng) — verified

Catalog chia ~90 chỉ tiêu Tín dụng theo **kỳ hạn**, ~66 theo **ngành**, ~23 theo
**phương thức** — nhưng fact khoản vay chỉ có agreement/customer/product/org.
Các thuộc tính đó nằm ở **contract**, nên projection reporting nay join
agreement → contract và fact thêm:

| Cột | Ý nghĩa |
|---|---|
| `loan_term_months` | kỳ hạn chuẩn hoá về tháng (DAY/30, YEAR*12) |
| `term_bucket` | `DEMAND` (0) · `SHORT` (≤12) · `MEDIUM_LONG` (>12) |
| `loan_method_code`, `industry_code`, `purpose_code` | phương thức / ngành / mục đích |

`term_bucket` tính trong SQL để ranh giới bucket là so sánh số đơn giản.

Seed nhóm kỳ hạn: 30002.01 (ngắn hạn), 30002.02 (trung dài hạn), 30002.03 (không
kỳ hạn) + tăng trưởng + TB 3 tháng. **Verified**: SHORT 600tr + MEDIUM_LONG
200tr + DEMAND 0 = **800tr** = tổng dư nợ; hai contract 12 và 24 tháng cho đúng
bucket.

**Cố ý chưa seed** nhóm **ngành** và **phương thức**: fact đã có `industry_code`
/`loan_method_code` và dimension đã khai báo, nhưng **chưa chốt taxonomy giá trị**
(mã nào là "nông nghiệp", mã nào là "từng lần") — đoán sẽ phân loại sai dư nợ
trên báo cáo. Chỉ cần thêm filter khi có taxonomy.

## 9i. Bước 6b-2 — tiền vay TCTD (domain mới)

Arda chưa có nghiệp vụ **đi vay** liên ngân hàng. Đã dựng trong deposit-service
(cạnh `ibm_deposits`, để hai chiều thị trường liên ngân hàng nằm cùng một service):

| Thành phần | Nội dung |
|---|---|
| Schema | `ibm_borrows` + `ibm_borrow_movements` |
| API | `GET/POST /api/deposit/borrows`, `GET /{id}`, `POST /{id}/decision`, `POST /{id}/movements` |
| Reporting | `GET /internal/reporting/ibm-borrows` (signed) |
| Fact | `rpt_fact_ibm_borrow_daily` |
| Chỉ tiêu | 11 (tổng, trong hạn/quá hạn, NHHTX, quỹ bảo toàn, số món, lãi suất BQ, theo kỳ hạn, lãi dự chi, tăng trưởng, TB 3 tháng) |

**Quyết định thiết kế:**

- **Vòng đời nhẹ hơn** bên cho vay: submit → `PENDING_APPROVAL`, checker APPROVE
  → `ACTIVE`. Giữ segregation of duties ở tầng API (**người nộp không được tự
  duyệt**) mà không cần BPMN. Cột `workflow_case_id` đã có sẵn nên gắn
  maker-checker engine sau **không cần đổi schema**.
- `lender_type` (NHHTX/NHNN/OTHER_TCTD/SAFETY_FUND) và `funding_purpose` là **cột
  thật**, không suy diễn — công thức PCF chia theo chúng.
- `maturity_status` (CURRENT/OVERDUE/SETTLED) do **ETL tính**, không để chỉ tiêu
  tự suy: engine so literal, không so ngày.
- Policy `/api/deposit/**` đã bao trùm endpoint mới (không cần route riêng).

**Còn lại**: FE cho nghiệp vụ vay (trang danh mục + form), BPMN maker-checker đầy
đủ nếu cần, và verify trên cluster.

### Verified trên cluster

- Submit → `PENDING_APPROVAL` (300tr, NHHTX, CREDIT_EXPANSION, kỳ hạn 12 tháng).
- **Maker tự duyệt → 403** `the submitter cannot approve their own borrowing`;
  checker duyệt → `ACTIVE`.
- ETL: `ibm_borrows: 1`; fact có `maturity_status=CURRENT` (đáo hạn 2027 > kỳ).
- Compute: **70000.01 = 300tr · 70000.02 = 300tr · 70000.03 = 0 · 70001.01 =
  300tr · 70005.01 = 1 · 70004.01 = 5.5 · 70000.01.03 = 150tr** (TB 3 tháng) —
  khớp fact.

## 9d. Bước 6 (member) — ĐÓNG, verified trên cluster (2026-09-22)

Luồng đầy đủ chạy thật: **đăng ký thành viên → yêu cầu góp vốn → maker SUBMIT
→ checker APPROVE → `ST_Execute` → vốn góp chuyển → case COMPLETED**.

Bằng chứng số:
- `POST /api/crm/members` → 201; `total_capital_minor` = 50.000.000.
- 2 yêu cầu ADDITIONAL 20.000.000 được duyệt → `add_capital_minor` = 40.000.000,
  `total_capital_minor` = **90.000.000** (khớp sổ).
- `RPT_EXTRACT_DAILY` → `members: 1, member_requests: 4`; fact ghi đúng, mỗi
  request một `request_key` riêng.
- `POST /api/statistical/indicators/compute?period_code=2026-09` → **25/25
  computed, 0 failed**; 12 chỉ tiêu vốn góp khớp fact (10024.04 = 90tr,
  10025.01 = 50tr, 10025.02 = 40tr, 10022.01 = 1).
- `GET /api/statistical/indicators/document` → PDF 29.913 bytes
  (`indicators-2026-09.pdf`, Gotenberg).
- `GET /api/statistical/reports/DEPOSIT_PORTFOLIO/export` → XLSX
  (`Content-Type: ...spreadsheetml.sheet`).
- `GET /api/statistical/reports/CUSTOMER_SUMMARY/run` → 1 dòng `RETAIL|ACTIVE|1`.

Còn lại của Bước 6: IBM borrow/deposit (37 chỉ tiêu), kho quỹ/chuyển tiền/TSCĐ;
Bước 7 (proactive agent).

Vận hành: commit code mà bị commit docs theo sau **ngay** thì GitHub Actions
cancel build của commit code → không có image cho commit đó; phải dispatch lại
`images.yml` trên tip. Đây là lý do phải pin tay thay vì chờ ImageUpdater.

Quy tắc rút ra: contract HTTP phải verify bằng **payload thật của FE**
(snake_case), không bằng struct Go; và expression SQL phải cast kiểu tường minh
khi cộng tham số; gọi service khác phải theo **đủ trường bắt buộc** như luồng
đã chạy, không chỉ trường mình thấy cần.

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
