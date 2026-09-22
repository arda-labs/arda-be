# Đối chiếu hệ tài khoản PCF ↔ COA pilot Arda

> Sinh tự động từ `docs/epas-survey/exports/pcf-kpi-catalog.json` (184 công thức
> kế toán dạng P) và `finance.fin_coa_accounts` (40 tài khoản, version V1).
> **Đây là tài liệu để chốt mapping — không phải mapping.** Mọi dòng phải
> được kế toán duyệt trước khi seed.

Tổng số tiền tố tài khoản được công thức PCF tham chiếu: **191**.

| Kết quả khớp tiền tố | Số tiền tố | Ý nghĩa |
|---|---|---|
| Khớp đúng 1 tài khoản Arda | 13 | Có thể map trực tiếp (vẫn cần xác nhận tên) |
| Khớp nhiều tài khoản (nhập nhằng) | 16 | **Prefix của PCF gộp nhiều tài khoản Arda — phải map tay** |
| Không có tài khoản Arda nào | 162 | **Thiếu tài khoản trong COA pilot** |

## Kết luận — đây không phải vấn đề mapping

**162/191 tiền tố PCF không có tài khoản nào trong COA pilot.** Đây không phải
chuyện dịch mã tài khoản: **Arda đơn giản là không có những tài khoản đó**.

Ví dụ: PCF cần `TK 21` (cho vay), `TK 30/305` (TSCĐ và hao mòn), `TK 20x/21x`
(phân loại nợ), `TK 40–49` (nợ phải trả / vốn), `TK 5x` (doanh thu),
`TK 6x` (chi phí)… COA pilot chỉ có `1321` cho tiền gửi liên ngân hàng, `1311`
cho vay, và một ít quỹ/doanh thu — **thiếu toàn bộ nhóm TSCĐ, phân loại nợ theo
nhóm, và phần lớn tài khoản nguồn vốn**.

Nói cách khác: **bảng mapping không cứu được**. Mapping chỉ dịch giữa hai hệ cùng
có tài khoản; ở đây một hệ thiếu gần hết tài khoản nên **không có gì để trỏ tới**.
Một chỉ tiêu "Nguyên giá TSCĐ" (`DCN TK 30`) không thể tính nếu sổ không theo dõi
TSCĐ.

### Hệ quả

- ~400 chỉ tiêu phụ thuộc tài khoản **chỉ tính được khi COA pilot có các tài
  khoản tương ứng** — tức phải **bổ sung/đổi COA**, không phải map.
- 13 tiền tố khớp đúng 1 tài khoản (bảng cuối) là phần **duy nhất** map được
  ngay — nhưng vẫn phải kiểm tên, vì trùng tiền tố không bảo đảm trùng nghĩa
  (PCF `TK 13` = tiền gửi TCTD, còn Arda `1321` mới là tiền gửi TCTD trong khi
  `1311` là cho vay).

### Khuyến nghị

Chọn một trong hai, và cả hai đều là **quyết định nghiệp vụ**, không phải code:

1. **Bổ sung COA pilot theo hệ QTDND** (khuyến nghị). Sổ hiện gần như trống (40
   tài khoản, 0 số dư), nên đây là thời điểm rẻ nhất. Sau đó chỉ tiêu chạy thẳng,
   không cần lớp dịch.
2. **Giữ COA riêng và chấp nhận phạm vi hẹp**: chỉ ~13 tiền tố map được, phần
   còn lại không báo cáo được cho tới khi tài khoản tồn tại.

Điều **không** nên làm: seed chỉ tiêu `account_balance` theo prefix hiện tại. Ví
dụ `TK 13` (tiền gửi tại TCTD khác) khớp `LIKE '13%'` sẽ hút luôn `1311` **Cho vay
khách hàng**, `1319` dự phòng và `13101` phải thu lãi — đặt số sai lên bảng cân
đối.


## Tiền tố KHÔNG khớp tài khoản nào — COA thiếu

| Tiền tố PCF | Số công thức dùng | Ví dụ công thức |
|---|---|---|
| `TK 1113` | 1 | 40096.01 |
| `TK 13111` | 3 | 40082.01, 40084.01, 40100.01 |
| `TK 1312` | 1 | 40095.01 |
| `TK 13121` | 3 | 40083.01, 40084.01, 40100.01 |
| `TK 139` | 5 | 40000.01, 40003.01, 40035.01 |
| `TK 20` | 5 | 40000.01, 40035.01, 40081.01 |
| `TK 209` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 21` | 2 | 40004.01, 40005.01 |
| `TK 211` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 212` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 213` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 219` | 5 | 40000.01, 40035.01, 40105.01 |
| `TK 251` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 252` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 253` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 259` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 281` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 282` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 283` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 284` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 285` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 289` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 291` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 292` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 293` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 299` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 30` | 2 | 40008.01, 40010.01 |
| `TK 301` | 6 | 40000.01, 40011.01, 40014.01 |
| `TK 302` | 6 | 40000.01, 40012.01, 40015.01 |
| `TK 303` | 6 | 40000.01, 40013.01, 40016.02 |
| `TK 305` | 5 | 40000.01, 40008.01, 40035.01 |
| `TK 3051` | 2 | 40009.01, 40014.01 |
| `TK 3052` | 1 | 40015.01 |
| `TK 3053` | 1 | 40016.02 |
| `TK 31` | 7 | 40000.01, 40017.02, 40018.02 |
| `TK 313001` | 1 | 40144.01 |
| `TK 313002` | 1 | 40144.01 |
| `TK 313003` | 1 | 40144.01 |
| `TK 313004` | 1 | 40145.01 |
| `TK 32` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 321` | 1 | 40146.01 |
| `TK 34` | 1 | 40006.01 |
| `TK 344` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 34401` | 1 | 40097.01 |
| `TK 349` | 5 | 40000.01, 40007.01, 40035.01 |
| `TK 351` | 5 | 40000.01, 40023.02, 40035.01 |
| `TK 352` | 5 | 40000.01, 40024.02, 40035.01 |
| `TK 3592` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 3599` | 7 | 40000.01, 40017.02, 40030.02 |
| `TK 36` | 5 | 40017.02, 40022.02, 40026.02 |
| `TK 361` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 3612` | 1 | 40098.01 |
| `TK 369` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 38` | 2 | 40018.02, 40021.02 |
| `TK 381` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 386` | 7 | 40000.01, 40017.02, 40030.02 |
| `TK 387` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 388` | 5 | 40000.01, 40035.01, 40105.01 |
| `TK 389` | 4 | 40000.01, 40035.01, 40105.01 |
| `TK 39` | 8 | 40000.01, 40028.02, 40029.02 |
| `TK 3911` | 1 | 40136.01 |
| `TK 3941` | 1 | 40135.01 |
| `TK 40` | 4 | 40036.01, 40037.01, 40048.01 |
| `TK 415` | 1 | 40040.01 |
| `TK 41511` | 1 | 40078.01 |
| `TK 41512` | 2 | 40077.01, 40078.01 |
| `TK 41513` | 2 | 40077.01, 40078.01 |
| `TK 41519` | 1 | 40079.01 |
| `TK 41591` | 1 | 40078.01 |
| `TK 41592` | 2 | 40077.01, 40078.01 |
| `TK 41593` | 2 | 40077.01, 40078.01 |
| `TK 41599` | 1 | 40079.01 |
| `TK 4232` | 2 | 40041.01, 40076.01 |
| `TK 4238` | 1 | 40076.02 |
| `TK 427` | 1 | 40076.02 |
| `TK 44` | 4 | 40036.01, 40042.01, 40048.01 |
| `TK 45` | 5 | 40036.01, 40043.01, 40046.01 |
| `TK 453` | 6 | 40000.01, 40022.02, 40027.02 |
| `TK 4538` | 1 | 40141.01 |
| `TK 46` | 5 | 40036.01, 40043.01, 40046.01 |
| `TK 48` | 5 | 40036.01, 40043.01, 40046.01 |
| `TK 4841` | 1 | 40121.01 |
| `TK 4842` | 1 | 40122.01 |
| `TK 4844` | 1 | 40123.01 |
| `TK 488` | 1 | 40140.01 |
| `TK 489` | 4 | 40036.01, 40047.01, 40048.01 |
| `TK 4892` | 7 | 40000.01, 40017.02, 40030.02 |
| `TK 4899` | 7 | 40000.01, 40017.02, 40030.02 |
| `TK 493` | 1 | 40138.01 |
| `TK 50` | 4 | 40036.01, 40046.01, 40048.01 |
| `TK 5199` | 2 | 40142.01, 40143.01 |
| `TK 60` | 4 | 40049.01, 40056.01, 40057.01 |
| `TK 601` | 1 | 40147.01 |
| `TK 601011` | 1 | 40133.01 |
| `TK 601012` | 1 | 40133.019999999997 |
| `TK 602` | 1 | 40148.01 |
| `TK 609` | 1 | 40050.01 |
| `TK 612` | 1 | 40120.01 |
| `TK 613` | 1 | 40149.019999999997 |
| `TK 69` | 1 | 40104.01 |
| `TK 692` | 4 | 40053.01, 40055.01, 40056.01 |
| `TK 70` | 2 | 40058.01, 40060.01 |
| `TK 701` | 1 | 40080.01 |
| `TK 702` | 1 | 40080.019999999997 |
| `TK 709` | 1 | 40080.03 |
| `TK 713` | 1 | 40089.01 |
| `TK 714` | 1 | 40090.01 |
| `TK 719` | 1 | 40091.01 |
| `TK 74` | 2 | 40064.01, 40066.01 |
| `TK 78` | 1 | 40067.01 |
| `TK 79` | 3 | 40064.01, 40066.01, 40080.04 |
| `TK 809` | 1 | 40087.229999999901 |
| `TK 819` | 1 | 40087.239999999903 |
| `TK 83` | 1 | 40087.249999999898 |
| `TK 831` | 1 | 40068.01 |
| `TK 832` | 1 | 40068.01 |
| `TK 833` | 1 | 40072.01 |
| `TK 8331` | 2 | 40102.019999999997, 40102.03 |
| `TK 84` | 3 | 40065.01, 40066.01, 40106.01 |
| `TK 85` | 1 | 40068.01 |
| `TK 851` | 1 | 40087.019999999997 |
| `TK 852` | 1 | 40087.03 |
| `TK 853` | 1 | 40087.040000000001 |
| `TK 854` | 1 | 40087.050000000003 |
| `TK 856` | 1 | 40087.06 |
| `TK 857` | 1 | 40087.07 |
| `TK 859` | 1 | 40087.08 |
| `TK 86` | 1 | 40068.01 |
| `TK 8611` | 1 | 40087.089999999997 |
| `TK 8612` | 1 | 40087.10 |
| `TK 8614` | 1 | 40087.109999999899 |
| `TK 8619` | 1 | 40087.119999999901 |
| `TK 862` | 1 | 40087.2599999999 |
| `TK 863` | 1 | 40087.269999999902 |
| `TK 865` | 1 | 40087.279999999897 |
| `TK 866` | 1 | 40087.289999999899 |
| `TK 868` | 1 | 40087.349999999802 |
| `TK 8691` | 1 | 40087.30 |
| `TK 8692` | 1 | 40087.359999999797 |
| `TK 8693` | 1 | 40087.309999999801 |
| `TK 8694` | 1 | 40087.319999999803 |
| `TK 8695` | 1 | 40087.359999999797 |
| `TK 8696` | 1 | 40087.359999999797 |
| `TK 8697` | 1 | 40087.329999999798 |
| `TK 8699` | 1 | 40087.3399999998 |
| `TK 87` | 1 | 40068.01 |
| `TK 871` | 1 | 40087.159999999902 |
| `TK 872` | 1 | 40087.179999999898 |
| `TK 87401` | 1 | 40087.169999999896 |
| `TK 87402` | 1 | 40087.1899999999 |
| `TK 876` | 1 | 40087.20 |
| `TK 882` | 1 | 40087.1499999999 |
| `TK 8822` | 1 | 40070.01 |
| `TK 8824` | 1 | 40068.01 |
| `TK 8825` | 1 | 40068.01 |
| `TK 8826` | 1 | 40068.01 |
| `TK 8829` | 1 | 40068.01 |
| `TK 883` | 1 | 40068.01 |
| `TK 88301` | 1 | 40087.129999999903 |
| `TK 88302` | 1 | 40087.139999999898 |
| `TK 89` | 2 | 40065.01, 40068.01 |
| `TK 9` | 1 | 40119.01 |

## Tiền tố khớp NHIỀU tài khoản — phải map tay

| Tiền tố PCF | Khớp vào | Số công thức |
|---|---|---|
| `TK 13` | 13101 Phải thu lãi cho vay; 1311 Cho vay khách hàng; 1319 Dự phòng phải thu khó đòi; 1321 Tiền gửi tại TCTD khác | 5 |
| `TK 35` | 35302 Quỹ thưởng cán bộ quản lý; 35303 Quỹ thưởng cho nhân viên; 35304 Quỹ phúc lợi hình thành TSCĐ; 35305 Quỹ phúc lợi ban quản lý điều hành | 4 |
| `TK 353` | 35302 Quỹ thưởng cán bộ quản lý; 35303 Quỹ thưởng cho nhân viên; 35304 Quỹ phúc lợi hình thành TSCĐ; 35305 Quỹ phúc lợi ban quản lý điều hành | 5 |
| `TK 41` | 411 Vốn đầu tư của chủ sở hữu; 418 Các Quỹ thuộc vốn chủ sở hữu; 41801 Quỹ đầu tư phát triển; 41802 Quỹ dự phòng tài chính; 41803 Quỹ dự trữ bổ sung vốn hoạt động | 4 |
| `TK 42` | 4211 Kết quả kinh doanh; 4231 Tiền gửi tiết kiệm | 4 |
| `TK 5` | 511 Doanh thu hoạt động (TT92); 5111 Doanh thu lãi cho vay; 5112 Doanh thu phí cho vay; 515 Doanh thu hoạt động tài chính | 4 |
| `TK 51` | 511 Doanh thu hoạt động (TT92); 5111 Doanh thu lãi cho vay; 5112 Doanh thu phí cho vay; 515 Doanh thu hoạt động tài chính | 4 |
| `TK 61` | 611 Chi phí hoạt động (TT92); 61112 Trích lập dự phòng rủi ro; 615 Chi phí tài chính | 3 |
| `TK 611` | 611 Chi phí hoạt động (TT92); 61112 Trích lập dự phòng rủi ro | 1 |
| `TK 7` | 711 Thu nhập khác (TT92); 71106 Hoàn lập dự phòng; 7111 Doanh thu cung cấp dịch vụ | 17 |
| `TK 71` | 711 Thu nhập khác (TT92); 71106 Hoàn lập dự phòng; 7111 Doanh thu cung cấp dịch vụ | 2 |
| `TK 711` | 711 Thu nhập khác (TT92); 71106 Hoàn lập dự phòng; 7111 Doanh thu cung cấp dịch vụ | 1 |
| `TK 8` | 8011 Chi phí lãi vốn nguồn; 8021 Chi phí lãi tiền gửi; 811 Chi phí khác (TT92); 8111 Chi phí kinh doanh khác; 821 Chi phí thuế thu nhập doanh nghiệp | 18 |
| `TK 80` | 8011 Chi phí lãi vốn nguồn; 8021 Chi phí lãi tiền gửi | 2 |
| `TK 81` | 811 Chi phí khác (TT92); 8111 Chi phí kinh doanh khác | 3 |
| `TK 811` | 811 Chi phí khác (TT92); 8111 Chi phí kinh doanh khác | 1 |

## Tiền tố khớp đúng 1 tài khoản — kiểm tra tên rồi xác nhận

| Tiền tố PCF | Tài khoản Arda | Số công thức |
|---|---|---|
| `TK 10` | 1011 Tiền mặt tại quỹ | 6 |
| `TK 11` | 1131 Tiền gửi ngân hàng | 4 |
| `TK 1311` | 1311 Cho vay khách hàng | 1 |
| `TK 2` | 22902 Dự phòng rủi ro chung | 2 |
| `TK 411` | 411 Vốn đầu tư của chủ sở hữu | 1 |
| `TK 421` | 4211 Kết quả kinh doanh | 2 |
| `TK 423` | 4231 Tiền gửi tiết kiệm | 1 |
| `TK 4231` | 4231 Tiền gửi tiết kiệm | 2 |
| `TK 49` | 4911 Lãi phải trả tiền gửi | 6 |
| `TK 491` | 4911 Lãi phải trả tiền gửi | 1 |
| `TK 642` | 642 Chi phí quản lý, kinh doanh | 3 |
| `TK 801` | 8011 Chi phí lãi vốn nguồn | 1 |
| `TK 802` | 8021 Chi phí lãi tiền gửi | 1 |
