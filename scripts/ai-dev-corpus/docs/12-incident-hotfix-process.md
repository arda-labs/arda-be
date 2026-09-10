# Quy trình xử lý sự cố và phát hành nóng (hotfix)

## Phân loại sự cố

Sev1: dịch vụ ngừng toàn bộ hoặc lộ dữ liệu - phản hồi 15 phút, escalation ngay tới trưởng nhóm oncall. Sev2: một tính năng chính hỏng, có giải pháp tạm - phản hồi 1 giờ. Sev3: lỗi nhỏ, không chặn vận hành - xử lý trong sprint kế tiếp.

## Khi có sự cố

Bước 1: khoanh vùng - kiểm tra dashboard và log của service liên quan. Bước 2: nếu cần rollback, dùng tính năng rollback của Argo CD về revision ổn định trước đó (argo app rollback), không sửa trực tiếp manifest trên cluster vì selfHeal sẽ ghi đè. Bước 3: mở kênh incident riêng, ghi lại timeline và mọi quyết định. Bước 4: sau khi ổn định, viết postmortem trong 3 ngày làm việc, tập trung nguyên nhân gốc không đổ lỗi cá nhân.

## Quy trình hotfix

Hotfix là nhánh riêng từ tag phát hành gần nhất, đặt tên hotfix/{mã-vấn-đề}. Chỉ được chứa thay đổi tối thiểu cho lỗi đang có. Phải vượt qua toàn bộ CI gates (kiểm thử, check scripts) trước khi merge. Sau khi merge vào main, image mới được Argo CD đồng bộ - theo dõi rollout tới khi mọi pod sẵn sàng.

## Kiểm soát phát hành

Mặc định phát hành vào giữa tuần (thứ Ba đến thứ Năm), tránh thứ Sáu chiều và cuối tuần trừ hotfix. Mỗi lần phát hành phải có ghi chú release với danh sách thay đổi và kế hoạch rollback. Sau phát hành 30 phút, người chịu trách nhiệm xác nhận các chỉ số (tỷ lệ lỗi, độ trễ) ổn định rồi mới đóng kênh incident.

## Liên hệ oncall

Lịch oncall luân phiên hằng tuần, tra cứu trong trang On-call. Mọi escalation Sev1 gọi trực tiếp điện thoại, không nhắn tin.
