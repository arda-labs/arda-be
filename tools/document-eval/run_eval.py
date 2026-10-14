"""Send each sample to document-service and score field accuracy."""
import argparse, json, re, statistics, sys, time, unicodedata
from datetime import datetime
from pathlib import Path

import requests

STRICT = ("id_number", "date_of_birth", "sex")


def expected(field, raw):
    raw = unicodedata.normalize("NFC", raw or "").strip()
    if field == "id_number":
        return re.sub(r"\D", "", raw) or None
    if field in ("date_of_birth", "issue_date", "expiry_date"):
        try:
            return datetime.strptime(raw, "%d/%m/%Y").strftime("%Y-%m-%d")
        except ValueError:
            return None  # "Không thời hạn" or empty
    if field == "sex":
        return {"Nam": "M", "Nữ": "F"}.get(raw)
    return raw or None


def same(field, want, got):
    if want is None or got is None:
        return want == got
    if field in ("id_number", "sex") or field.endswith("date") or field == "date_of_birth":
        return want == got
    norm = lambda s: re.sub(r"\s+", " ", unicodedata.normalize("NFC", s)).casefold().strip(" .,")
    return norm(want) == norm(got)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default="http://localhost:8100")
    ap.add_argument("--samples", default="samples")
    a = ap.parse_args()
    root = Path(a.samples)
    gt_all = json.loads((root / "gt.json").read_text(encoding="utf-8"))
    per_field, secs, errors, version_ok = {}, [], [], 0
    for sid, gt in gt_all.items():
        files = {"front": (f"{sid}_front.jpg", (root / f"{sid}_front.jpg").read_bytes(), "image/jpeg"),
                 "back": (f"{sid}_back.jpg", (root / f"{sid}_back.jpg").read_bytes(), "image/jpeg")}
        t = time.time()
        resp = requests.post(f"{a.base_url}/api/documents/extract", data={"doc_type": "cccd"}, files=files, timeout=60)
        secs.append(time.time() - t)
        if resp.status_code != 200:
            errors.append(f"{sid}: HTTP {resp.status_code} {resp.text[:200]}")
            for f in gt["fields"]:
                per_field.setdefault(f, []).append(False)
            continue
        res = resp.json()["result"]
        version_ok += res["card_version"] == gt["card_version"]
        for f, raw in gt["fields"].items():
            want, got = expected(f, raw), res["fields"][f]["value"]
            ok = same(f, want, got)
            per_field.setdefault(f, []).append(ok)
            if not ok:
                errors.append(f"{sid}.{f}: want {want!r} got {got!r} flags {res['fields'][f]['flags']}")
    n = len(gt_all)
    total = sum(sum(v) for v in per_field.values()) / sum(len(v) for v in per_field.values())
    print(f"cards {n} | card_version {version_ok}/{n} | all fields {100 * total:.1f}% | "
          f"sec mean {statistics.mean(secs):.1f} p95 {sorted(secs)[max(0, int(len(secs) * .95) - 1)]:.1f}")
    for f, v in per_field.items():
        print(f"  {f:16s} {sum(v)}/{len(v)}")
    for e in errors[:40]:
        print("   ", e)
    strict_ok = all(all(per_field[f]) for f in STRICT)
    if not strict_ok or total < 0.90:
        print("FAIL: spec §3.5 thresholds not met")
        sys.exit(1)
    print("PASS")


if __name__ == "__main__":
    main()
