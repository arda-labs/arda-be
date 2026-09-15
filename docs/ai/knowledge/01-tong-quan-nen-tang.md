# Tổng quan nền tảng Arda

Arda là nền tảng quản trị nghiệp vụ hợp nhất cho tổ chức tài chính: một cổng
web duy nhất (shell) gắn kết nhiều module nghiệp vụ độc lập, dùng chung đăng
nhập, phân quyền, tổ chức và hạ tầng.

## Các module nghiệp vụ

- **IAM (`/admin`)** — người dùng, nhóm, vai trò, quyền, phiên đăng nhập và
  audit bảo mật.
- **Platform (`/admin`)** — dữ liệu nền: tổ chức, tham số hệ thống, danh mục
  tra cứu, lịch làm việc, biểu mẫu.
- **CRM (`/customers`, `/workbench`)** — hồ sơ khách hàng hội viên, dự án,
  bàn làm việc xử lý giao dịch.
- **HRM (`/hrm`)** — chức danh, đơn vị, hồ sơ nhân viên.
- **Finance (`/finance`)** — hệ thống tài khoản kế toán, bút toán, số dư.
- **Deposit (`/deposit`)** — tiền gửi tiết kiệm, sản phẩm, nghiệp vụ liên
  ngân hàng.
- **Loan (`/loans`)** — cho vay: sản phẩm, giải ngân, thu nợ.
- **Capital (`/capital`)** — quản lý vốn: hợp đồng, quỹ, sản phẩm vốn.
- **Workflow (`/workflow`)** — quy trình nghiệp vụ (BPMN), công việc, hạn xử
  lý (SLA), giám sát tiến trình.
- **MDM (`/admin/mdm`)** — danh mục chuẩn: tiền tệ, quốc gia, lãi suất.
- **Statistical (`/statistical`)** — định nghĩa báo cáo, chỉ tiêu, kỳ nộp.
- **Media, Notification** — lưu trữ tệp và thông báo nội bộ.
- **Account (`/my-account`)** — hồ sơ cá nhân, bảo mật, thiết bị.

## Trợ lý AI Olorin

Olorin là trợ lý AI tích hợp sẵn trong shell (bảng bên phải, mở rộng toàn màn
hình bằng Ctrl/Cmd+J):

- Hỏi bằng tiếng Việt tự nhiên; Olorin tự gọi đúng chức năng hệ thống để tra
  cứu dữ liệu thật trong tenant của bạn.
- Trả lời tài liệu nội bộ **kèm trích dẫn nguồn** (tài liệu nào, đoạn nào).
- Mọi hành động ghi/xuất dữ liệu đều chuyển thành **đề xuất chờ con người phê
  duyệt** trước khi thực thi.
- Olorin chỉ thấy dữ liệu bạn có quyền; không có quyền thì công cụ tương ứng
  không xuất hiện.

## Mô hình phân quyền (tóm tắt)

- Mỗi người dùng thuộc một hoặc nhiều tổ chức (tenant) và đăng nhập vào một
  tenant đang hoạt động.
- Quyền được gán qua vai trò/nhóm; quyền toàn hệ thống (global) tách riêng
  với quyền trong tenant.
- Mọi yêu cầu từ trình duyệt đi qua cổng xác thực trung tâm; các dịch vụ nội
  bộ không tin dữ liệu danh tính do client gửi.

## Quy trình phổ biến

- **Quản lý người dùng**: tạo user → gán vào nhóm/tổ chức → vai trò quyết
  định quyền truy cập từng module.
- **Nghiệp vụ có phê duyệt**: giao dịch/hồ sơ được tạo dưới dạng case trong
  Workflow; người tạo (maker) và người kiểm tra (checker) độc lập nhau; mọi
  bước có dấu vết audit.
- **Tra cứu**: danh sách dùng tìm kiếm/lọc/phân trang; số liệu tổng hợp nằm
  trong module thống kê hoặc dashboard từng nghiệp vụ.
