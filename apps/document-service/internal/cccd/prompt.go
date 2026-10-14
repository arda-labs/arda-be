package cccd

// Prompt is sent with the card images. Keys are snake_case; the model returns
// raw strings and the service normalizes them. Keep it in sync with
// tools/document-eval when card layouts change.
const Prompt = `Bạn nhận 1 hoặc 2 ảnh thẻ căn cước của Việt Nam. Ảnh thứ nhất là mặt trước, ảnh thứ hai (nếu có) là mặt sau.
Đọc thông tin in trên thẻ và trả về DUY NHẤT một object JSON, không giải thích, không markdown.

Các khóa, luôn có đủ, giá trị là chuỗi, không thấy thì để "":
- "card_version": một trong "cccd_chip_2021" (thẻ "CĂN CƯỚC CÔNG DÂN" gắn chip, mặt sau có chip và dòng ký tự máy đọc), "can_cuoc_2024" (thẻ "CĂN CƯỚC" mẫu từ 01/07/2024), "cccd_barcode" (thẻ "CĂN CƯỚC CÔNG DÂN" có mã vạch), "unknown" (thẻ căn cước nhưng không rõ mẫu), "not_cccd" (không phải thẻ căn cước hoặc căn cước công dân của Việt Nam).
- "image_quality": "ok", hoặc "low" nếu ảnh mờ, lóa, bị che hoặc bị cắt mất chữ.
- "id_number": số căn cước công dân / số định danh cá nhân, 12 chữ số, giữ nguyên các số 0 ở đầu.
- "full_name": họ, chữ đệm và tên.
- "date_of_birth": ngày sinh, dạng dd/mm/yyyy.
- "sex": "Nam" hoặc "Nữ".
- "nationality": quốc tịch.
- "place_of_origin": quê quán (thẻ mẫu 2024 không có, để "").
- "residence": nơi thường trú hoặc nơi cư trú.
- "issue_date": ngày cấp, dạng dd/mm/yyyy.
- "issue_place": nơi cấp: chức danh người ký hoặc tên cơ quan cấp in trên thẻ.
- "expiry_date": ngày hết hạn hoặc "có giá trị đến", dạng dd/mm/yyyy; nếu thẻ ghi "Không thời hạn" thì trả đúng chuỗi "Không thời hạn".

Quy tắc:
- Chép đúng từng chữ như in trên thẻ, giữ nguyên dấu tiếng Việt và chữ hoa, chữ thường. Không sửa chính tả, không đoán chữ bị che.
- Ngày cấp và nơi cấp nằm ở mặt sau. Với thẻ mẫu 2024, nơi cư trú và ngày hết hạn cũng nằm ở mặt sau.
- Không lấy thông tin từ dòng ký tự máy đọc ở mặt sau nếu thông tin đó đã in rõ ở chỗ khác.`
