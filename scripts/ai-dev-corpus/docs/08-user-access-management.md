# Quy trình đăng ký người dùng và phân quyền

## Vòng đời tài khoản

Tài khoản được HR hoặc admin tạo qua module IAM. Trạng thái lần lượt: DRAFT (chưa kích hoạt) → ACTIVE (đã xác thực email và đặt mật khẩu) → SUSPENDED (tạm khóa vi phạm hoặc nghỉ việc) → ARCHIVED (lưu trữ sau 90 ngày nghỉ). Xóa cứng chỉ áp dụng cho tài khoản chưa bao giờ đăng nhập.

## Đăng nhập

Đăng nhập đi qua Hydra (OAuth2 server) và Kratos (identity server). Sau xác thực, auth-gateway cấp session cookie và phiên dịch quyền của người dùng thành header cho các service phía sau. Session hết hạn sau 8 giờ không hoạt động; refresh token kéo dài tối đa 7 ngày.

## Vai trò và quyền

Quyền theo mô hình RBAC: người dùng thuộc vai trò, vai trò gắn permission. Quyền định danh dạng {tên miền}.{đối tượng}.{hành động} ví dụ crm.customer.read, ai.knowledge.manage, finance.approval.write. Quản trị viên toàn cục (global admin) vượt qua kiểm tra quyền theo tenant nhưng vẫn bị ghi audit.

## Tenant

Mọi dữ liệu nghiệp vụ được cách ly theo tenant_id. Tenant đầu tiên của hệ thống có định danh 00000000-0000-0000-0000-000000000010. Người dùng chỉ thấy dữ liệu của tenant mình thuộc; chuyển tenant cần đăng nhập lại.

## Phê duyệt trong hệ thống

Các luồng có rủi ro cao (duyệt khoản vay, duyệt xuất dữ liệu khách hàng, duyệt mua sắm trên hạn mức) yêu cầu người phê duyệt khác người tạo (four-eyes principle) và hết hạn sau 72 giờ không xử lý.
