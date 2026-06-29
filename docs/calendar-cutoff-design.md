# Phân hệ Ngày làm việc & Giờ chốt sổ (Business Calendar & Cut-off Time)

Tài liệu này đặc tả thiết kế và kiến trúc triển khai của phân hệ quản lý **Ngày giao dịch hệ thống (Business Date)** và **Giờ chốt sổ nhận giao dịch (Cut-off Time)** trong `finance-service` của Arda.

---

## 1. Tổng quan Nghiệp vụ

Trong các hệ thống tài chính - ngân hàng, ngày hạch toán kế toán không luôn đồng nhất với ngày vật lý (Physical Date). Phân hệ này đảm bảo:
*   **Business Date:** Ngày làm việc chính thức của hệ thống tài chính dùng để hạch toán kế toán.
*   **Holidays & Weekends:** Bỏ qua Thứ Bảy, Chủ Nhật và các ngày nghỉ lễ được khai báo trong Database để xác định chính xác ngày làm việc tiếp theo.
*   **Cut-off Time:** Quy định giờ chốt sổ cho từng kênh giao dịch và loại nghiệp vụ. Các giao dịch xảy ra sau thời điểm này sẽ tự động được hạch toán vào ngày làm việc tiếp theo ($T+1$).
*   **EOD (End of Day):** Tiến trình đóng sổ cuối ngày, dịch chuyển ngày làm việc hiện tại thành ngày làm việc tiếp theo.

---

## 2. Cơ sở Dữ liệu (Database Schema)

Các bảng dữ liệu được tạo tại file migration `20260629165500_add_calendar.sql`:

### 2.1 Bảng Ngày làm việc Hệ thống (`fin_system_dates`)
Theo dõi ngày làm việc hiện tại, trước đó và kế tiếp cho từng chi nhánh.
*   `branch_code` (Khóa chính): Thường mặc định là `HEAD_OFFICE`.
*   `current_business_date`: Ngày hạch toán hiện tại ($T$).
*   `previous_business_date`: Ngày hạch toán của kỳ trước ($T-1$).
*   `next_business_date`: Ngày hạch toán dự kiến của kỳ kế tiếp ($T+1$).
*   `status`: Trạng thái ngày (`OPEN`, `EOD_PROCESSING`, `CLOSED`).

### 2.2 Bảng Lịch Nghỉ Lễ (`fin_holiday_calendars`)
Khai báo ngày nghỉ lễ để hệ thống tự động bỏ qua khi tính ngày làm việc kế tiếp.
*   `holiday_date`: Ngày lễ cụ thể (YYYY-MM-DD).
*   `is_recurring`: `TRUE` nếu ngày này lặp lại hàng năm (ví dụ Quốc khánh Dương lịch cố định).

### 2.3 Bảng Cấu hình Giờ chốt sổ (`fin_cutoff_configs`)
*   `channel_code`: Kênh giao dịch (Ví dụ: `CITAD`, `NAPAS`, `COUNTER`).
*   `transaction_type`: Loại nghiệp vụ (Ví dụ: `TRANSFER`, `DEPOSIT`).
*   `cutoff_time`: Giờ chốt sổ (ví dụ: `16:30:00`).

---

## 3. Các API Endpoints

Các API được đăng ký dưới tiền tố `/api/finance/calendar/`:

*   **Lấy trạng thái lịch hệ thống hiện tại:**
    ```http
    GET /api/finance/calendar/status?branchCode=HEAD_OFFICE
    ```
*   **Đánh giá ngày hạch toán cho giao dịch:**
    ```http
    GET /api/finance/calendar/evaluate?channel=CITAD&type=TRANSFER&time=2026-06-29T16:35:00Z
    ```
    *Trả về ngày hạch toán phù hợp dựa vào giờ cut-off cấu hình.*
*   **Chạy xử lý cuối ngày (EOD) thủ công/tự động:**
    ```http
    POST /api/finance/calendar/eod?branchCode=HEAD_OFFICE
    ```
*   **Danh sách ngày nghỉ lễ:**
    ```http
    GET /api/finance/calendar/holidays
    ```
*   **Thêm ngày nghỉ lễ mới:**
    ```http
    POST /api/finance/calendar/holidays
    Body:
    {
      "date": "2026-09-02",
      "description": "Quốc Khánh Việt Nam",
      "isRecurring": true
    }
    ```

---

## 4. Giải thuật & Logic Nghiệp vụ Lõi

Sơ đồ hoạt động và cấu trúc nghiệp vụ được tổ chức tại lớp `CalendarService`:

1.  **Hàm `EvaluateAccountingDate`:**
    *   Lấy cấu hình giờ chốt sổ của `(channel_code, transaction_type)`.
    *   So sánh thời gian giao dịch thực thi `executionTime` với mốc chốt sổ trong ngày.
    *   Nếu trễ hơn, trả về `system_dates.next_business_date`. Ngược lại, trả về `system_dates.current_business_date`.

2.  **Hàm `calculateNextBusinessDay`:**
    *   Tự động cộng thêm ngày liên tiếp cho đến khi tìm được ngày không rơi vào Thứ Bảy, Chủ Nhật và không nằm trong danh sách `fin_holiday_calendars`.
