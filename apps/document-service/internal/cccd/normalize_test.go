package cccd

import (
	"slices"
	"testing"
	"time"
)

func val(f Field) string {
	if f.Value == nil {
		return "<nil>"
	}
	return *f.Value
}

func TestTextFieldNFCAndWhitespace(t *testing.T) {
	decomposed := "Thùy" // "Thùy" with combining grave
	f := textField("  HOÀNG   " + decomposed + " \n")
	if val(f) != "HOÀNG Thùy" || *f.Raw != val(f) || len(f.Flags) != 0 {
		t.Fatalf("got %q raw %v flags %v", val(f), f.Raw, f.Flags)
	}
	if f.Flags == nil {
		t.Fatal("flags must be an empty slice, not nil (JSON [])")
	}
	empty := textField("   ")
	if empty.Value != nil || empty.Raw != nil || !slices.Equal(empty.Flags, []string{FlagNotFound}) {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestIDField(t *testing.T) {
	if f := idField("082 279 855 585"); val(f) != "082279855585" || *f.Raw != "082 279 855 585" {
		t.Fatalf("got %q", val(f))
	}
	if f := idField("không rõ"); f.Value != nil || !slices.Contains(f.Flags, FlagNotFound) {
		t.Fatalf("no digits = %+v", f)
	}
}

func TestDateField(t *testing.T) {
	f, d := dateField("26/05/1988")
	if val(f) != "1988-05-26" || d == nil || !d.Equal(time.Date(1988, 5, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %q %v", val(f), d)
	}
	if f, _ := dateField("6-5-1988"); val(f) != "1988-05-06" {
		t.Fatalf("dashes: %q", val(f))
	}
	f, d = dateField("31/02/1990")
	if f.Value != nil || d != nil || *f.Raw != "31/02/1990" || !slices.Contains(f.Flags, FlagInvalidDate) {
		t.Fatalf("invalid = %+v", f)
	}
	if f, _ := dateField(""); !slices.Contains(f.Flags, FlagNotFound) {
		t.Fatalf("empty = %+v", f)
	}
}

func TestExpiryField(t *testing.T) {
	for _, raw := range []string{"Không thời hạn", "KHÔNG THỜI HẠN", "khong thoi han"} {
		f, d, none := expiryField(raw)
		if !none || f.Value != nil || d != nil || !slices.Equal(f.Flags, []string{FlagNoExpiry}) {
			t.Fatalf("%q => %+v", raw, f)
		}
	}
	if f, d, none := expiryField("26/05/2048"); none || val(f) != "2048-05-26" || d == nil {
		t.Fatalf("date expiry = %+v", f)
	}
}

func TestSexField(t *testing.T) {
	for raw, want := range map[string]string{"Nam": "M", "nam": "M", "Male": "M", "Nữ": "F", "NỮ": "F", "Female": "F"} {
		if got := val(sexField(raw)); got != want {
			t.Errorf("%q => %q", raw, got)
		}
	}
	if f := sexField("Khác"); f.Value != nil || !slices.Contains(f.Flags, FlagInvalidFormat) {
		t.Fatalf("other = %+v", f)
	}
	if f := sexField(""); !slices.Contains(f.Flags, FlagNotFound) {
		t.Fatalf("empty = %+v", f)
	}
}

func TestNormalizeCardVersion(t *testing.T) {
	cases := map[string]string{
		"cccd_chip_2021": CardChip2021, " CAN_CUOC_2024 ": CardCanCuoc2024, "cccd_barcode": CardBarcode,
		"not_cccd": cardNotCCCD, "passport": CardUnknown, "": CardUnknown,
	}
	for in, want := range cases {
		if got := normalizeCardVersion(in); got != want {
			t.Errorf("%q => %q", in, got)
		}
	}
}
