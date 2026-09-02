# FAQ — Cách sử dụng hệ thống Arda và trợ lý AI Olorin

Bộ câu hỏi thường gặp về cách làm việc với trợ lý AI Olorin trên nền tảng Arda.
Nội dung này được nạp vào kho tri thức của trợ lý qua `knowledge-indexer`
(docs-as-code) — chỉnh sửa tại `arda-be/docs/ai/knowledge/` và chạy lại indexer.

## Olorin là gì?

Olorin là trợ lý AI của nền tảng Arda. Bạn hỏi bằng tiếng Việt tự nhiên, Olorin
tự tìm và gọi đúng chức năng của hệ thống (tra cứu dữ liệu, tạo đề xuất xuất
dữ liệu, tìm tài liệu nội bộ...) rồi trả lời kèm dữ liệu thật từ tenant của bạn.
Mọi thao tác đọc chỉ nằm trong phạm vi dữ liệu tenant mà bạn có quyền; mọi hành
động ghi dữ liệu đều phải con người phê duyệt trước khi thực thi.

## Làm sao biết Olorin có thể làm gì?

Hỏi trực tiếp, ví dụ: "Bạn có thể làm được những gì?", "Liệt kê các chức năng mà
trợ lý hỗ trợ". Olorin sẽ gọi danh mục khả năng (`listCapabilities`) và trả về
danh sách chức năng hiện có kèm quyền yêu cầu. Bạn cũng có thể lọc theo lĩnh vực
(crm, finance, hrm, knowledge, iam) hoặc theo loại (đọc dữ liệu / ghi dữ liệu).

## Cách tra cứu danh sách user?

Hỏi: "Cho tôi danh sách user", "Liệt kê các users thuộc tenant này". Olorin trả
về danh sách user trong tenant của bạn (trang đầu, kèm phân trang nếu cần). Có
thể kèm điều kiện như trạng thái hoạt động: "liệt kê user đang bị vô hiệu hóa".

## Cách xem thông tin của tôi?

Hỏi: "Tôi có những quyền gì?", "Tôi là ai trong hệ thống này?". Olorin trả về
thông tin tài khoản đang đăng nhập: user, tenant, tổ chức đang hoạt động, vai
trò và quyền hạn. Thông tin này lấy từ context xác thực của bạn, không truy vấn
dịch vụ ngoài.

## Cách tra cứu thông tin khách hàng?

Hỏi kèm mã khách hàng, ví dụ: "Xem thông tin khách C-7". Olorin trả về hồ sơ
khách hàng từ hệ thống CRM: tên, mã, thông tin liên hệ, mức độ rủi ro và các
thuộc tính được lưu.

## Cách xuất dữ liệu khách hàng?

Hỏi: "Xuất dữ liệu khách C-7 ra file", "Tải hồ sơ khách C-7 về máy dưới dạng
file". Vì đây là thao tác ghi/xuất dữ liệu, Olorin tạo một **đề xuất** kèm định
dạng (CSV/JSON) và chờ bạn phê duyệt — không có dữ liệu nào được xuất khi chưa
có người đồng ý. Sau khi duyệt, file được chuẩn bị theo định dạng đã chọn.

## Cách tra cứu tài khoản kế toán?

Hỏi kèm mã tài khoản, ví dụ: "Tài khoản kế toán ACC-1 số dư hiện tại bao
nhiêu?". Olorin truy vấn hệ thống tài chính và trả về thông tin tài khoản:
mã, tên, trạng thái, số dư hiện tại.

## Cách tra cứu thông tin nhân viên?

Hỏi: "Liệt kê nhân viên phòng kinh doanh", "Tìm nhân viên theo phòng ban".
Lưu ý: dữ liệu nhân viên thuộc hệ thống HRM; nếu bạn cần thông tin tài khoản
đăng nhập hệ thống thì đó là "user" (xem mục tra cứu danh sách user).

## Cách tra tài liệu, FAQ hoặc quy trình nội bộ?

Hỏi: "Tra FAQ về cách sử dụng hệ thống", "Quy trình duyệt hợp đồng được quy
định thế nào?". Olorin tìm trong kho tri thức đã được đăng tải cho tenant và
trả lời kèm trích dẫn nguồn (tên tài liệu, đoạn trích). Nội dung kho tri thức
được nạp từ các file markdown do đội ngũ duy trì — không có nội dung nào được
"tự sinh".

## Tại sao có thao tác phải chờ phê duyệt?

Mọi hành động thay đổi hoặc ghi dữ liệu (mutation — xuất file, cập nhật, xóa...)
đều được chuyển thành đề xuất chờ con người phê duyệt trước khi thực thi. Đây
là chính sách an toàn của nền tảng: AI đề xuất, con người quyết định. Đề xuất
có thời hạn hiệu lực; hết hạn bạn cần yêu cầu lại.

## Khi nào Olorin trả lời "chưa có dữ liệu"?

Khi nguồn dữ liệu được hỏi trống hoặc chưa có nội dung được đăng tải (ví dụ kho
tri thức chưa nạp tài liệu nào), Olorin sẽ báo thẳng rằng chưa có dữ liệu phù
hợp thay vì đoán hoặc bịa câu trả lời. Trong trường hợp đó, hãy liên hệ đội quản
trị để bổ sung nội dung, hoặc thử lại sau.
