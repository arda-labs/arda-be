# Đặc tả Kiến trúc Bảo mật & Mã hóa Doanh nghiệp (Enterprise Security & Crypto Specification)

Tài liệu quy định kiến trúc bảo mật nhiều lớp (Defense-in-Depth), tiêu chuẩn mã hóa duy nhất **AES-256-GCM** kết hợp **HKDF-SHA256 (RFC 5869)**, cơ chế tìm kiếm trên dữ liệu đã mã hóa (**Blind Indexing**), quản lý khóa (**KMS/Vault**), và hàng rào phòng thủ mạng chống SSRF trong nền tảng Arda AI.

---

## 1. Tiêu chuẩn Mã hóa Thống nhất (Single Enterprise Standard)

Hệ thống Arda chuẩn hóa **DUY NHẤT 1 CÔNG THỨC MÃ HÓA** cho toàn bộ dữ liệu nhạy cảm at-rest:

```
┌──────────────────────────────────────────────────────────────────────────┐
│                    CHUẨN VÀNG ENTERPRISE DUY NHẤT                        │
├───────────────────────────────┬──────────────────────────────────────────┤
│ Thuật toán Mã hóa (Cipher)    │ AES-256-GCM (Galois/Counter Mode)        │
│ Dẫn xuất Khóa (Key Derivation)│ HKDF-SHA256 (RFC 5869 - IETF Standard)   │
│ Kích thước Khóa (Key Size)    │ 256-bit (Kháng máy tính lượng tử)        │
│ Tìm kiếm (Blind Index)        │ HMAC-SHA256 with Isolated Salt           │
│ Định dạng Lưu trữ (Format)    │ enc:v1:<base64-url(nonce+ciphertext+tag)>│
└───────────────────────────────┴──────────────────────────────────────────┘
```

### 💡 Lý do Lựa chọn Chuẩn này:
1. **Tuân thủ Tuyệt đối (Compliance & Audit Ready):** Đáp ứng 100% tiêu chuẩn bắt buộc của **SOC 2 Type II, ISO 27001, PCI-DSS v4.0, HIPAA, và FIPS 140-3**.
2. **Tương thích Native với Cloud KMS:** AWS KMS, Google Cloud KMS, Azure Key Vault, và HashiCorp Vault đều sử dụng AES-256-GCM làm chuẩn Envelope Encryption mặc định.
3. **Tăng tốc Phần cứng (AES-NI):** 100% CPU Intel Xeon, AMD EPYC, ARM Neoverse (AWS Graviton) thực thi trực tiếp trên vi lệnh phần cứng, tốc độ > 10 GB/s, độ trễ < 1 micro-giây.

---

## 2. Mô hình Phân tầng Bảo mật (Security Architecture Tiers)

```
┌────────────────────────────────────────────────────────────────────────┐
│ 1. CLIENT / BROWSER TIER                                               │
│    - API Key Write-Only & Masking (chỉ hiển thị sk-...9f8a)            │
│    - Phân quyền RBAC (Chỉ role ai.admin / superadmin mới thấy Cài đặt)  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ HTTPS (TLS 1.3 / HSTS)
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 2. GATEWAY & AUTH TIER                                                 │
│    - Server-Resolved Scope (X-Tenant-Id, X-User-Id, X-Auth-Checked)   │
│    - Phân quyền bất biến: Client không thể giả mạo TenantID           │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Internal mTLS
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 3. APPLICATION & CRYPTO TIER (libs/go/arda-crypto)                     │
│    - Envelope Encryption: AES-256-GCM với HKDF-SHA256 (RFC 5869)       │
│    - Format chuẩn: enc:v1:<base64(nonce + ciphertext + tag)>          │
│    - Blind Indexing: bidx:v1:<hex(HMAC-SHA256(val, salt))>           │
│    - Egress Guard (arda-http): Chặn SSRF vào Cloud Metadata & Private │
│    - ClientPool (ai-service): In-Memory LRU Cache (128 entries, 15m)  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ SQL (Chỉ lưu Ciphertext)
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ 4. DATABASE TIER (PostgreSQL 18)                                       │
│    - Bảng public.ai_tenant_settings lưu Ciphertext (enc:v1:...)        │
│    - TDE (Transparent Data Encryption) trên Cloud Native EBS Volume    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Thư viện Dùng chung: `arda-crypto` (`libs/go/arda-crypto`)

Mọi dịch vụ trong hệ thống Arda (`ai-service`, `iam-service`, `crm-service`, `payment-service`) khi cần lưu trữ thông tin bí mật (API Keys, OAuth Secrets, CCCD, Thẻ ngân hàng) **BẮT BUỘC** phải sử dụng thư viện `github.com/arda-labs/arda/libs/go/arda-crypto`.

### 3.1 Dẫn xuất Khóa: HKDF-SHA256 (RFC 5869)
- Sử dụng cơ chế 2 bước **Extract-and-Expand**:
  - `hkdfExtract(salt, masterSecret)` ➔ PRK (Pseudorandom Key)
  - `hkdfExpand(prk, info, 32)` ➔ Khóa con 256-bit độc lập
- Đảm bảo nguyên tắc "Một khóa - Một nhiệm vụ": Khóa dùng cho mã hóa dữ liệu (`"data-encryption"`) hoàn toàn độc lập toán học với Khóa dùng cho tìm kiếm (`"blind-index"`).

### 3.2 Thuật toán AES-256-GCM
- **Thuật toán:** Galois/Counter Mode (GCM) với khối khóa 256-bit (AES-256).
- **Tính xác thực (Authenticated Encryption):** Tự động phát hiện và từ chối giải mã nếu dữ liệu bị sửa đổi (tampering) dù chỉ 1 bit.
- **Nonce:** Mỗi lần mã hóa tạo 1 nonce ngẫu nhiên 96-bit (12 bytes) từ CSPRNG (`crypto/rand`). Tuyệt đối không tái sử dụng nonce.
- **Cấu trúc lưu trữ:**
  ```
  enc:v1:<base64-url-encoded( nonce[12] + ciphertext[N] + auth_tag[16] )>
  ```

### 3.3 Blind Indexing (Tìm kiếm trên Dữ liệu đã Mã hóa)
- Khi dữ liệu được mã hóa bằng AES-256-GCM với random nonce, cùng một giá trị (ví dụ: email `ceo@corp.com`) sẽ sinh ra các chuỗi ciphertext khác nhau.
- **Giải pháp:** Sử dụng hàm `ardacrypto.BlindIndex(plaintext, salt)`:
  - Chuẩn hóa: `lowercase(trim(plaintext))`
  - Hash: `HMAC-SHA256(normalized, DerivedKey(salt, "blind-index"))`
  - Format: `bidx:v1:<64-hex-chars>`
- Cho phép query nhanh trong Database:
  ```sql
  SELECT * FROM users WHERE email_bidx = $1;
  ```

### 3.4 Adapter Quản lý Khóa (KMS / Vault Integration)
- Gói `ardacrypto` cung cấp interface `KeyProvider`:
  ```go
  type KeyProvider interface {
      GetSecretKey(ctx context.Context, keyID string) (string, error)
  }
  ```
- **Môi trường Development:** Sử dụng `StaticKeyProvider` nạp từ `ARDA_SERVICE_AUTH_SECRET`.
- **Môi trường Production:** Triển khai adapter gọi **AWS KMS**, **HashiCorp Vault Transit Engine**, hoặc **Google Cloud KMS** để tự động xoay vòng khóa (Key Rotation).

---

## 4. Bộ lọc Egress An toàn Mạng Chống SSRF (`arda-http`)

Hàm `ardahttp.ValidateEgressURL(rawURL string, allowLocal bool)` bảo vệ hệ thống khỏi các cuộc tấn công Server-Side Request Forgery (SSRF) khi gọi API bên ngoài:

| Dải IP / Hostname | Mục đích / Nguy cơ | Hành vi Production (`allowLocal=false`) | Hành vi Dev (`allowLocal=true`) |
|:---|:---|:---:|:---:|
| `169.254.169.254` | AWS / GCP Instance Metadata Service | 🚫 **CHẶN TUYỆT ĐỐI** | 🚫 **CHẶN TUYỆT ĐỐI** |
| `metadata.google.internal` | GCP Metadata Service | 🚫 **CHẶN TUYỆT ĐỐI** | 🚫 **CHẶN TUYỆT ĐỐI** |
| `0.0.0.0` | Linux Unspecified binding | 🚫 **CHẶN TUYỆT ĐỐI** | 🚫 **CHẶN TUYỆT ĐỐI** |
| `127.0.0.1` / `localhost` | Loopback nội bộ | 🚫 CHẶN | ✅ CHO PHÉP (Ollama test) |
| `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | Private Subnet nội bộ | 🚫 CHẶN | ✅ CHO PHÉP |
| `https://api.openai.com/...` | Public Internet HTTPS | ✅ CHO PHÉP | ✅ CHO PHÉP |

---

## 5. Bảng Quy định Bảo mật cho Toàn bộ Dữ liệu AI

| Dữ liệu | Vị trí Lưu trữ | Phương pháp Bảo mật | Ai được phép xem? |
|:---|:---|:---|:---|
| **AI Provider API Key** | `public.ai_tenant_settings.api_key` | AES-256-GCM + HKDF-SHA256 (`enc:v1:...`) | Chỉ giải mã trong RAM Backend khi gọi LLM. Frontend chỉ nhận `sk-...9f8a`. |
| **Audit Logs Phê duyệt** | `public.ai_approvals` | Append-only, SHA-256 Idempotency Key | `ai.admin`, `superadmin` |
| **Lịch sử Chat & Tool Calls** | `public.ai_messages`, `ai_tool_executions` | Tự động Redact token/auth headers (`[REDACTED]`) | Tenant User sở hữu cuộc trò chuyện |
| **RAG Knowledge Base** | `public.ai_knowledge_chunks` | Tenant ID Isolation + Row Level Security | Người dùng có quyền `ai.knowledge.read` |
