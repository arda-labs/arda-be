# Thiết kế dữ liệu flow duyệt CRM/HRM trên arda (db-per-service)

> Phạm vi: đối chiếu mô hình dữ liệu 4 tầng Txn → Inf → Audit → History của
> EPAS với kiến trúc arda (mỗi service một DB riêng, frontend không chạm DB),
> trả lời câu hỏi "cần thêm bảng nào để flow CRM/HRM đủ logic như EPAS mà
> không phá ranh giới service".

## 1. Mô hình EPAS (khảo sát từ code, `docs/epas-survey`)

EPAS dùng **một schema Oracle/PG dùng chung** cho mọi module, với quy ước
tiền tố 4 tầng cho dữ liệu nghiệp vụ có duyệt:

| Tầng | Bảng (CRM) | Nội dung | Sinh lúc nào |
|---|---|---|---|
| **TXN** (staging) | `CRM_TXN_CUST` (+ `_INDV/_CORP`, `_DOC`, `_SEG`, `_RELN`) | Bản ghi đang trình duyệt — maker nhập, checker duyệt trên đây | Starter tạo ngay khi submit case, gắn `process_instance_code` |
| **INF** (master) | `CRM_INF_CUST` (+ children) | Bản ghi chính thức — "trạng thái kích hoạt" chính là presence trong INF | Worker END_PROCESS migrate khi `approvalResult = APPROVE` |
| **AUDIT** | `CRM_INF_CUST_A` | Ai sửa gì khi nào (`DATA_TIME`) | Cùng lúc migrate |
| **HISTORY** | `CRM_INF_CUST_H` | Snapshot đóng băng theo ngày (`customer_code + data_date`) | Cùng lúc migrate |

HRM y hệt: `HRM_TXN_EMPLOYEE` → `HRM_INF_EMPLOYEE` (+ position/family/edu/auth
con) → `_A` → `_H` (TransactionServiceImpl.updateEndProcess, be_hrm).

Điểm mấu chốt của EPAS: **bản ghi business chưa tồn tại "thật" cho đến khi
duyệt xong**; mọi edit lại sau duyệt đi một vòng TXN mới. STATUS trên TXN
(`BUSINESS_STATUS`) và trạng thái BPM (`BPM_TXN_PROCESS_INSTANCE`) tách bạch.

## 2. Mô hình arda hiện tại

- **db-per-service thật**: `crm`, `hrm`, `workflow`, `finance`, `iam`… là các
  database Postgres riêng (đã bootstrap trên cluster). Không service nào được
  đụng DB service khác; hợp tác qua gRPC.
- **CRM**: 1 bảng `customers` (status: DRAFT/SUBMITTED/NEEDS_CHANGES/ACTIVE/
  REJECTED) + `customer_amendments` — bảng này **đã đúng tinh thần TXN**:
  có `before_snapshot`/`after_snapshot` JSONB + `changed_fields`, applied/
  rejected metadata, unique pending per customer. Khi APPROVE, amendment
  được apply đè lên `customers`.
- **HRM**: `hrm_employee_registrations` (draft|submitted|approved|rejected)
  riêng với `hrm_employees`; đăng ký duyệt xong mới tạo employee — tương đương
  "TXN trước, INF sau" nhưng **chưa có audit/history**.
- **workflow-service**: giữ toàn bộ trạng thái quy trình (`business_cases`,
  `workflow_tasks`, timeline, SLA, work items) — tương đương `BPM_TXN_*` của
  EPAS, kèm `business_cases.timeline` đã là một dạng audit.

## 3. Đối chiếu & khoảng trống

| Khả năng | EPAS | arda hiện tại | Đánh giá |
|---|---|---|---|
| Staging trước duyệt | `*_TXN` bảng riêng | `customer_amendments` (CRM), `hrm_employee_registrations` (HRM) | ✅ Đủ — khác tên, cùng chức năng |
| Snapshot trước/sau | Trải trên nhiều bảng con, không gọn | JSONB 1 cột + `changed_fields[]` | ✅ Tốt hơn cho UI diff; đủ truy vết |
| Ai duyệt, khi nào | `_A` bảng audit tách rời | Cột `applied_by/at`, `rejected_by/at` + timeline case | ⚠️ Đủ cho CRM; HRM chưa có trường nào |
| Snapshot lịch sử theo ngày | `*_H` + `data_date` | Không có | ❌ Không thể trả lời "khách hàng/hồ sơ NV này thời điểm T trông thế nào" |
| Dữ liệu master chỉ-through-INF | Presence trong `*_INF` | `customers.status=ACTIVE`, `hrm_employees` row | ✅ Tương đương về ngữ nghĩa |

## 4. Thiết kế đề xuất — giữ db-per-service, băng gọn 4 tầng EPAS

Nguyên tắc: **không copy 4 tầng bảng EPAS** (nó là hệ quả của 1 schema dùng
chung; copy nguyên trạng vào arda sẽ nhân đôi bảng con và tạo join chéo
service). Thay vào đó, mỗi service tự sở hữu đủ 3 lớp thông tin trong DB của
mình, nằm gọn trong schema riêng:

### 4.1 CRM (đã gần đạt, bổ sung nhỏ)

Giữ nguyên `customers` (INF) + `customer_amendments` (TXN). Bổ sung:

```sql
-- audit truy vết ở tầng INF (tương đương CRM_INF_CUST_A nhưng là 1 bảng
-- event duy nhất thay vì 1 bảng đối chiếu cho mỗi entity)
CREATE TABLE customer_audit_events (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    customer_id VARCHAR(64) NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    event_type  VARCHAR(32) NOT NULL,          -- CREATED | AMENDMENT_APPLIED | AMENDMENT_REJECTED | STATUS_CHANGED
    actor       VARCHAR(100) NOT NULL,
    case_id     VARCHAR(64),                   -- workflow case nguồn (nếu có)
    diff        JSONB NOT NULL DEFAULT '{}',   -- before/after hoặc metadata
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX customer_audit_events_customer_idx ON customer_audit_events (customer_id, occurred_at DESC);
```

- Ghi 1 event tại mỗi điểm: submit (worker execute/cancel/approve đã có), và
  khi apply amendment. Đây chính là `_A` của EPAS nhưng trung tính, không cần
  bảng audit riêng cho từng entity con.
- `customer_amendments.applied_at/by` + `diff` chính là "snapshot đóng băng"
  của mỗi lần đổi — thay cho tầng `*_H`. Nếu sau này cần history đúng nghĩa
  temporal (tra giá trị tại thời điểm T), thêm bảng event-sourced tổng quát:
  giữ nguyên thiết kế hiện tại, chỉ thêm bảng nếu yêu cầu thực xuất hiện.

### 4.2 HRM (thiếu nhiều hơn — làm cùng lúc với BPMN flow)

`hrm_employee_registrations` đã là TXN. Bổ sung cho parity:

```sql
-- INF giữ nguyên hrm_employees; thêm audit event như CRM
CREATE TABLE hrm_employee_audit_events (
    id             VARCHAR(64) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    employee_id    VARCHAR(64),                -- NULL khi sự kiện thuộc registration chưa duyệt
    registration_id VARCHAR(64) NOT NULL REFERENCES hrm_employee_registrations(id) ON DELETE CASCADE,
    event_type     VARCHAR(32) NOT NULL,       -- SUBMITTED | REVIEWED | APPROVED | REJECTED | EMPLOYEE_CREATED | EMPLOYEE_UPDATED
    actor          VARCHAR(100) NOT NULL,
    case_id        VARCHAR(64),
    payload        JSONB NOT NULL DEFAULT '{}',-- registration snapshot tại thời điểm sự kiện
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- Vì registration chưa có `before/after_snapshot` như CRM, snapshot đưa vào
  `payload` của event tại từng bước — đó là dữ liệu "đã trình duyệt gì".
- Sau khi BPMN HRM flow (mục tiếp theo) được duyệt, worker approve sẽ:
  1) insert `hrm_employees` từ registration (TXN→INF), 2) ghi event
  `EMPLOYEE_CREATED`, 3) return cho workflow-service mark case COMPLETED.

### 4.3 Ràng buộc kiến trúc cần giữ

1. **Chỉ service owner ghi vào DB của mình.** workflow-service không bao giờ
   update `customers`/`hrm_employees` — nó chỉ phát job; domain worker của
   service tương ứng mới ghi. (EPAS làm 1 SQL duy nhất được vì chung schema;
   arda cố ý tách — mất atomicity 1-transaction, chấp nhận idempotent retry:
   job Zeebe retries=3 + worker idempotent theo `caseId`.)
2. **Idempotency cho worker**: approve/execute phải no-op an toàn khi chạy
   lại (check status trước khi ghi, hoặc ON CONFLICT) — điều kiện bắt buộc
   khi thiếu cross-service transaction.
3. **Audit event là append-only** — không update/delete (down migration chỉ
   DROP toàn bảng). Không lưu FK chéo DB: `case_id` dạng string tham chiếu
   logic, không REFERENCES.
4. **Tên bảng**: đặt trong DB crm/hrm, prefix `customer_`/`hrm_employee_` —
   nhất quán quy ước hiện có; không dùng prefix `*_TXN/_INF/_A/_H` của EPAS.

## 5. Vì sao không làm EPAS-4-tầng đầy đủ ngay

- 4 tầng của EPAS tồn tại để bù cho schema dùng chung; arda đã có ranh giới
  service + `customer_amendments`/`hrm_employee_registrations` đóng vai TXN
  tốt hơn (JSONB snapshot + changed_fields) — nhân bản 4 tầng sẽ trùng lặp.
- Tầng History temporal (`_H` theo `data_date`) chỉ có giá trị khi nghiệp vụ
  báo cáo đòi hỏi "giá trị tại thời điểm"; khi đòi hỏi đó xuất hiện, bảng
  event append-only ở trên đủ để dựng lại mọi thời điểm (event sourcing
  một cách nhẹ nhàng) mà không phải migrate lại dữ liệu.

## 6. Việc cần làm khi implement (thứ tự)

1. (Đã vá) CRM adjustment worker gap — flow chạy trọn được.
2. CRM: thêm `customer_audit_events` + ghi event trong worker approve/reject
   và apply amendment. Migration không phá dữ liệu hiện có.
3. HRM: thêm `hrm_employee_audit_events` + snapshot; BPMN
   `hrm-employee-registration-v2` + workers (review/approve/reject) gọi
   hrm-service; approve tạo employee + event; repoint case type.
4. UI: HRM remote trang duyệt (dùng pattern CRM registration-page), workbench
   link into task context.
5. Mail reject (notification worker thật) — parity cuối của CRM.200/201,
   HRM.200/201 trước khi sang loan/fac.
