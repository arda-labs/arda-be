# Hướng Dẫn Kiến Trúc: Hệ Thống Xuất Dữ Liệu Lớn Chuẩn Ngân Hàng (Large-Scale Enterprise Export Engine)

Tài liệu này quy chuẩn kiến trúc, quy trình triển khai và các biện pháp bảo vệ khi thực hiện xuất dữ liệu lớn (từ 10.000 đến hàng triệu bản ghi) cho toàn bộ hệ sinh thái backend (Go) và frontend (React MFE) của Arda.

---

## 1. Bối cảnh & Thách thức Kỹ thuật

Khi xuất dữ liệu báo cáo trong hệ thống tài chính - ngân hàng:
1. **Tràn RAM (Out-Of-Memory / OOMKilled)**: Các thư viện Excel thông thường (như DOM-based builders) nạp toàn bộ cây đối tượng của hàng trăm nghìn dòng vào RAM. Nếu 3 người dùng cùng xuất báo cáo 200.000 dòng, server có thể tiêu tốn 4GB - 8GB heap RAM và làm crash pod backend.
2. **HTTP 504 Gateway Timeout**: Các reverse proxy (Cloudflare, AWS ALB, Nginx) ngắt kết nối sau 30s - 60s nếu request không phản hồi byte đầu tiên.
3. **Nghẽn Database Connection Pool**: Truy vấn quét bảng không kiểm soát làm khóa bảng (table locks) hoặc chiếm dụng toàn bộ connections của các giao dịch quan trọng.
4. **Mất toàn vẹn dữ liệu Ngân hàng**: Số tài khoản, số CCCD, mã CIF bị mất số 0 ở đầu (ví dụ: `0012345` biến thành `12345`) hoặc bị chuyển sang dạng số khoa học (`1.23E+15`).

---

## 2. Kiến trúc 3 Phân tầng (3-Tier Export Architecture)

Hệ thống tự động phân loại quy mô dữ liệu theo 3 phân tầng xử lý:

```
┌────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                       PHÂN TẦNG QUY MÔ DỮ LIỆU                                         │
└───────────────────────────────────────────────────┬────────────────────────────────────────────────────┘
                                                    │
        ┌───────────────────────────────────────────┼───────────────────────────────────────────┐
        ▼                                           ▼                                           ▼
┌───────────────────────────────┐   ┌───────────────────────────────┐   ┌───────────────────────────────┐
│       PHÂN TẦNG 1: NHỎ        │   │       PHÂN TẦNG 2: VỪA        │   │        PHÂN TẦNG 3: LỚN       │
│        (≤ 5.000 dòng)         │   │    (5.000 – 50.000 dòng)      │   │ (> 50.000 – 1.000.000+ dòng)  │
├───────────────────────────────┤   ├───────────────────────────────┤   ├───────────────────────────────┤
│ • Vị trí: Client-side / Sync  │   │ • Vị trí: Backend Direct Pipe │   │ • Vị trí: Async Worker Queue  │
│ • Thời gian: 1 - 2 giây       │   │ • Thời gian: 3 - 10 giây      │   │ • Thời gian: 30s - 3 phút     │
│ • Nạp: Tải trực tiếp browser  │   │ • Nạp: HTTP Chunked Stream    │   │ • Nạp: Ghi S3/MinIO & Push Bell│
│ • RAM Backend: 0 MB           │   │ • RAM Backend: Cố định ~25MB  │   │ • RAM Backend: Cố định ~30MB  │
└───────────────────────────────┘   └───────────────────────────────┘   └───────────────────────────────┘
```

---

## 3. Thư viện dùng chung Golang (`libs/go/arda-export`)

Thư viện `github.com/arda-labs/arda/libs/go/arda-export` cung cấp các công cụ cốt lõi:

### A. Zero-Allocation Streaming Writer (`StreamXLSX`)
- Sử dụng **`excelize.NewStreamWriter`** để ghi nén XML trực tiếp ra `io.Writer`.
- **Độ phức tạp bộ nhớ**: $O(1)$ RAM không phụ thuộc vào số lượng dòng.
- **Hiệu năng thực tế (Benchmark trên AMD Ryzen 5)**:
  - 10.000 bản ghi XLSX: hoàn thành trong **542ms**.
  - Bộ nhớ tiêu thụ ổn định, Garbage Collector không bị dừng luồng (no STW spikes).

### B. Direct HTTP Chunked Streaming (`ServeStreamHTTP`)
- Kết nối luồng đọc từ DB Cursor với luồng ghi HTTP qua **`io.Pipe()`**.
- Trả về ngay lập tức HTTP header `Transfer-Encoding: chunked` và `Content-Type`, ngăn chặn hoàn toàn lỗi HTTP Gateway Timeout.
- Tự động bắt sự kiện client ngắt kết nối (`r.Context().Done()`) để hủy query DB, tránh lãng phí tài nguyên.

### C. Chuẩn hóa Định dạng Ngân hàng (Banking Formatting Standards)
- **`CellTypeCode`**: Đánh dấu cell kiểu String để bảo toàn số `0` ở đầu (Số tài khoản, CIF, CCCD, SĐT).
- **`CellTypeCurrency` / `CellTypeNumber`**: Ghi số thực với định dạng phân cách hàng nghìn `#,##0`, cho phép tính công thức `=SUM()`.
- **`CellTypeDate`**: Định dạng ngày giờ chuẩn địa phương (`02/01/2006 15:04:05`).
- **`CellTypeBoolean`**: Tự động chuyển đổi thành "Có"/"Không" (vi-VN) hoặc "Yes"/"No" (en-US).
- **Header Audit Watermark & Freeze Panes**: Tự động chèn tiêu đề báo cáo, dòng metadata audit (thời gian, người xuất, phạm vi) và cố định dòng tiêu đề cột (`freezePanes`) ở dòng 4.

---

## 4. Hướng dẫn Triển khai cho một Microservice mới

Khi muốn bổ sung tính năng xuất dữ liệu cho một service khác (ví dụ: `finance-service`, `crm-service`):

### Bước 1: Thêm Repository Streaming Method
Trong `repository`, viết hàm trả về `*sql.Rows` không dùng `LIMIT`/`OFFSET`:

```go
func (r *TransactionRepository) StreamTransactions(ctx context.Context, params FilterParams) (*sql.Rows, error) {
    query := `SELECT id, code, account_number, amount, status, created_at FROM finance_transactions WHERE ... ORDER BY created_at DESC`
    return r.db.QueryContext(ctx, query, args...)
}
```

### Bước 2: Viết Handler gọi `ardaexport.ServeStreamHTTP`
Trong `handler`:

```go
func (h *TransactionHandler) ExportTransactions(w http.ResponseWriter, r *http.Request) {
    format := ardaexport.NormalizeFormat(r.URL.Query().Get("format"))
    filename := fmt.Sprintf("transactions_%s", time.Now().Format("20060102_150405"))

    cols := []ardaexport.Column{
        {Header: "Mã giao dịch", Key: "code", Type: ardaexport.CellTypeCode},
        {Header: "Số tài khoản", Key: "accountNumber", Type: ardaexport.CellTypeCode},
        {Header: "Số tiền", Key: "amount", Type: ardaexport.CellTypeCurrency},
        {Header: "Trạng thái", Key: "status", Type: ardaexport.CellTypeString},
        {Header: "Thời gian", Key: "createdAt", Type: ardaexport.CellTypeDate},
    }

    opts := ardaexport.StreamOptions{
        Title:     "BÁO CÁO DANH SÁCH GIAO DỊCH TÀI CHÍNH",
        SheetName: "Transactions",
        Columns:   cols,
        Locale:    "vi-VN",
    }

    _ = ardaexport.ServeStreamHTTP(w, r, format, filename, func(ctx context.Context, out io.Writer) error {
        rows, err := h.repo.StreamTransactions(ctx, params)
        if err != nil {
            return err
        }
        defer rows.Close()

        supplier := func() ([]any, error) {
            if !rows.Next() {
                return nil, io.EOF
            }
            var id, code, acc, status string
            var amount float64
            var createdAt time.Time
            if err := rows.Scan(&id, &code, &acc, &amount, &status, &createdAt); err != nil {
                return nil, err
            }
            return []any{code, acc, amount, status, createdAt}, nil
        }

        if format == ardaexport.FormatCSV {
            return ardaexport.StreamCSV(ctx, out, opts, supplier)
        }
        return ardaexport.StreamXLSX(ctx, out, opts, supplier)
    })
}
```

### Bước 3: Đăng ký Route trong Router
```go
mux.HandleFunc("/api/finance/transactions/export", method("GET", transactionHandler.ExportTransactions))
```

---

## 5. Danh mục Kiểm thử và Đảm bảo Chất lượng (QA Checklist)

- [x] **Kiểm tra độ toàn vẹn số 0 ở đầu**: Các mã `"0012345678"` không bị Excel tự ý cắt thành `12345678`.
- [x] **Kiểm tra tính toán số tiền**: Các cột tiền tệ có thể sử dụng hàm `=SUM(C5:C1000)` bình thường trên Excel.
- [x] **Kiểm tra hiển thị Tiếng Việt UTF-8**: Mở trực tiếp file CSV trên Microsoft Excel không bị vỡ font nhờ ký tự UTF-8 BOM (`\uFEFF`).
- [x] **Kiểm tra ngắt kết nối Client**: Khi người dùng đóng tab hoặc hủy download, Backend ngắt query DB ngay lập tức mà không để lại connection rò rỉ.
