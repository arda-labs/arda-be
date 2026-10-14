"""Generate fictional CCCD / Căn cước cards (front + back) with ground truth.

Every card carries a "MẪU THỬ" watermark and no emblem/portrait/QR/chip.
"""
import argparse, json, random
from datetime import date, timedelta
from pathlib import Path

import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageFont

PROVINCE_CODES = ["001", "008", "019", "024", "031", "038", "040", "042", "046", "048", "051", "056",
                  "066", "068", "075", "079", "083", "089", "092", "094", "096"]
HO = ["Nguyễn", "Trần", "Lê", "Phạm", "Hoàng", "Huỳnh", "Phan", "Vũ", "Võ", "Đặng", "Bùi", "Đỗ", "Hồ", "Ngô", "Dương", "Lý"]
DEM = ["Thị", "Văn", "Hữu", "Đức", "Ngọc", "Thanh", "Quốc", "Minh", "Thùy", "Xuân", "Bảo", "Khánh", "Diệu", "Trọng"]
TEN = ["Hoàng", "Phượng", "Nguyệt", "Dũng", "Hằng", "Tuấn", "Khuê", "Quỳnh", "Thắng", "Hiếu", "Trường", "Yến",
       "Lực", "Huyền", "Tưởng", "Nhung", "Đạt", "Hưng"]
TINH = ["Thừa Thiên Huế", "Quảng Ngãi", "Đắk Lắk", "Hà Tĩnh", "Bà Rịa - Vũng Tàu", "Thái Nguyên", "Sóc Trăng",
        "Nghệ An", "Hưng Yên", "Khánh Hòa", "Bắc Giang", "Cà Mau"]
XA = ["Xã Ea Kao", "Phường Nghĩa Lộ", "Xã Quỳnh Lưu", "Phường Phú Hội", "Xã Điện Dương", "Thị trấn Đức Thọ"]
HUYEN = ["Huyện Krông Pắc", "Thành phố Huế", "Huyện Đức Thọ", "Thị xã Điện Bàn", "Huyện Quỳnh Lưu", "Huyện Tư Nghĩa"]
DUONG = ["Lê Duẩn", "Nguyễn Huệ", "Trần Hưng Đạo", "Hùng Vương", "Điện Biên Phủ", "Võ Nguyên Giáp"]
ISSUER = {
    "cccd_chip_2021": "CỤC TRƯỞNG CỤC CẢNH SÁT QUẢN LÝ HÀNH CHÍNH VỀ TRẬT TỰ XÃ HỘI",
    "cccd_barcode": "CỤC TRƯỞNG CỤC CẢNH SÁT ĐKQL CƯ TRÚ VÀ DLQG VỀ DÂN CƯ",
    "can_cuoc_2024": "BỘ CÔNG AN",
}
MIN_ISSUE = {"cccd_barcode": date(2016, 1, 1), "cccd_chip_2021": date(2021, 1, 1), "can_cuoc_2024": date(2024, 7, 1)}
MAX_ISSUE = {"cccd_barcode": date(2020, 12, 31), "cccd_chip_2021": date(2024, 6, 30), "can_cuoc_2024": date(2026, 9, 30)}


def fonts(font_dir):
    for cand in [font_dir, "C:/Windows/Fonts", "/usr/share/fonts/truetype/dejavu"]:
        if not cand:
            continue
        p = Path(cand)
        for reg, bold in [("arial.ttf", "arialbd.ttf"), ("DejaVuSans.ttf", "DejaVuSans-Bold.ttf")]:
            if (p / reg).exists() and (p / bold).exists():
                return lambda s, b=False: ImageFont.truetype(str(p / (bold if b else reg)), s)
    raise SystemExit("no Vietnamese-capable font found; pass --font-dir")


def rand_date(r, lo, hi):
    return lo + timedelta(days=r.randint(0, (hi - lo).days))


def fmt(d):
    return d.strftime("%d/%m/%Y")


def make_identity(r, version):
    sex = r.choice(["Nam", "Nữ"])
    dob = rand_date(r, date(1955, 1, 1), date(2006, 12, 28))
    digit = (dob.year // 100 - 19) * 2 + (1 if sex == "Nữ" else 0)
    id_number = r.choice(PROVINCE_CODES) + str(digit) + f"{dob.year % 100:02d}" + "".join(str(r.randint(0, 9)) for _ in range(6))
    lo = max(MIN_ISSUE[version], date(dob.year + 14, dob.month, min(dob.day, 28)))
    issue = rand_date(r, lo, MAX_ISSUE[version]) if lo < MAX_ISSUE[version] else MAX_ISSUE[version]
    milestones = [14, 25, 40, 60] if version == "can_cuoc_2024" else [25, 40, 60]
    expiry = "Không thời hạn"
    for m in milestones:
        try:
            at = date(dob.year + m, dob.month, dob.day)
        except ValueError:  # 29 Feb in a non-leap target year
            at = date(dob.year + m, 2, 28)
        if at > issue + timedelta(days=730):
            expiry = fmt(at)
            break
    return {
        "id_number": id_number, "full_name": f"{r.choice(HO)} {r.choice(DEM)} {r.choice(TEN)}".upper(),
        "date_of_birth": fmt(dob), "sex": sex, "nationality": "Việt Nam",
        "place_of_origin": "" if version == "can_cuoc_2024" else f"{r.choice(XA)}, {r.choice(HUYEN)}, {r.choice(TINH)}",
        "residence": f"Số {r.randint(2, 199)} {r.choice(DUONG)}, {r.choice(XA)}, {r.choice(HUYEN)}, {r.choice(TINH)}",
        "issue_date": fmt(issue), "issue_place": ISSUER[version], "expiry_date": expiry,
    }


def watermark(img, F):
    layer = Image.new("RGBA", img.size, (0, 0, 0, 0))
    ImageDraw.Draw(layer).text((img.width * 0.08, img.height * 0.42), "MẪU THỬ - KHÔNG CÓ GIÁ TRỊ",
                               font=F(img.width // 18, True), fill=(200, 30, 30, 55))
    return Image.alpha_composite(img.convert("RGBA"), layer.rotate(18)).convert("RGB")


def card(lines, F, bg=(232, 240, 236)):
    img = Image.new("RGB", (1300, 820), bg)
    d = ImageDraw.Draw(img)
    for xy, text, size, bold, color in lines:
        d.text(xy, text, font=F(size, bold), fill=color)
    return watermark(img, F)


def front(gt, version, F):
    ink, red = (20, 20, 20), (170, 20, 20)
    head = [((420, 40), "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", 30, True, ink),
            ((520, 80), "Độc lập - Tự do - Hạnh phúc", 26, True, ink)]
    if version == "can_cuoc_2024":
        rows = [((540, 130), "CĂN CƯỚC", 42, True, red),
                ((370, 210), "Số định danh cá nhân / Personal identification number:", 20, False, ink),
                ((370, 238), gt["id_number"], 38, True, ink),
                ((370, 300), "Họ, chữ đệm và tên khai sinh / Full name:", 20, False, ink),
                ((370, 328), gt["full_name"], 30, True, ink),
                ((370, 390), f"Ngày, tháng, năm sinh / Date of birth: {gt['date_of_birth']}", 24, False, ink),
                ((370, 432), f"Giới tính / Sex: {gt['sex']}", 24, False, ink),
                ((370, 474), f"Quốc tịch / Nationality: {gt['nationality']}", 24, False, ink)]
    else:
        rows = [((470, 130), "CĂN CƯỚC CÔNG DÂN", 40, True, red),
                ((370, 200), "Số / No.:", 20, False, ink), ((370, 228), gt["id_number"], 38, True, ink),
                ((370, 285), "Họ và tên / Full name:", 20, False, ink), ((370, 313), gt["full_name"], 30, True, ink),
                ((370, 370), f"Ngày sinh / Date of birth: {gt['date_of_birth']}", 24, False, ink),
                ((370, 410), f"Giới tính / Sex: {gt['sex']}", 24, False, ink),
                ((370, 450), f"Quốc tịch / Nationality: {gt['nationality']}", 24, False, ink),
                ((370, 495), "Quê quán / Place of origin:", 20, False, ink), ((370, 522), gt["place_of_origin"], 24, False, ink),
                ((370, 570), "Nơi thường trú / Place of residence:", 20, False, ink), ((370, 597), gt["residence"], 24, False, ink),
                ((50, 640), "Có giá trị đến:", 20, False, ink), ((50, 668), gt["expiry_date"], 24, False, ink)]
    img = card(head + rows, F)
    ImageDraw.Draw(img).rectangle((50, 230, 330, 580), outline=(120, 120, 120), width=3)
    return img


def back(gt, version, F):
    ink = (20, 20, 20)
    if version == "can_cuoc_2024":
        rows = [((60, 60), "Nơi cư trú / Place of residence:", 20, False, ink), ((60, 88), gt["residence"], 24, False, ink),
                ((60, 150), "Nơi đăng ký khai sinh / Place of birth:", 20, False, ink),
                ((60, 178), "Thành phố Hà Nội", 24, False, ink),
                ((60, 240), f"Ngày, tháng, năm cấp / Date of issue: {gt['issue_date']}", 24, False, ink),
                ((60, 282), f"Ngày, tháng, năm hết hạn / Date of expiry: {gt['expiry_date']}", 24, False, ink),
                ((700, 360), gt["issue_place"], 28, True, ink), ((700, 400), "MINISTRY OF PUBLIC SECURITY", 20, False, ink)]
    else:
        rows = [((60, 60), "Đặc điểm nhân dạng / Personal identification:", 20, False, ink),
                ((60, 88), "Nốt ruồi C:1cm trên đuôi mắt trái", 24, False, ink),
                ((60, 160), f"Ngày, tháng, năm / Date, month, year: {gt['issue_date']}", 24, False, ink),
                ((60, 220), gt["issue_place"], 22, True, ink)]
    rows.append(((60, 700), f"IDVNM{gt['id_number'][-9:]}<<0MAUTHU<<<<<<<<<<<<", 30, False, ink))
    return card(rows, F, bg=(236, 236, 232))


def photo(img, r):
    pad = int(max(img.size) * 0.12)
    bg = Image.new("RGB", (img.width + 2 * pad, img.height + 2 * pad), tuple(r.randint(70, 130) for _ in range(3)))
    bg.paste(img, (pad, pad))
    bg = bg.rotate(r.uniform(-5, 5), resample=Image.BICUBIC, fillcolor=bg.getpixel((2, 2)))
    a = np.asarray(bg).astype(np.float32)
    a *= np.linspace(r.uniform(0.75, 0.9), r.uniform(1.0, 1.1), bg.width)[None, :, None]
    a += np.random.default_rng(r.randint(0, 9999)).normal(0, 6, a.shape)
    out = Image.fromarray(np.clip(a, 0, 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(r.uniform(0.6, 1.1)))
    return out.resize((int(out.width * 0.85), int(out.height * 0.85)), Image.LANCZOS)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="samples")
    ap.add_argument("--per-version", type=int, default=6)
    ap.add_argument("--seed", type=int, default=20261014)
    ap.add_argument("--font-dir")
    a = ap.parse_args()
    r, F, out = random.Random(a.seed), fonts(a.font_dir), Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    gt_all = {}
    for version in ["cccd_chip_2021", "can_cuoc_2024", "cccd_barcode"]:
        for i in range(a.per_version):
            sid = f"{version}_{i:02d}"
            gt = make_identity(r, version)
            photo(front(gt, version, F), r).save(out / f"{sid}_front.jpg", quality=r.randint(60, 80))
            photo(back(gt, version, F), r).save(out / f"{sid}_back.jpg", quality=r.randint(60, 80))
            gt_all[sid] = {"card_version": version, "fields": gt}
    (out / "gt.json").write_text(json.dumps(gt_all, ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"{len(gt_all)} cards -> {out}")


if __name__ == "__main__":
    main()
