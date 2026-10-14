package cccd

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// cleanText applies NFC (models mix composed and decomposed Vietnamese marks)
// and collapses whitespace.
func cleanText(s string) string {
	return strings.Join(strings.Fields(norm.NFC.String(s)), " ")
}

func ptr(s string) *string { return &s }

func notFound() Field { return Field{Flags: []string{FlagNotFound}} }

func textField(raw string) Field {
	raw = cleanText(raw)
	if raw == "" {
		return notFound()
	}
	return Field{Value: ptr(raw), Raw: ptr(raw), Flags: []string{}}
}

func idField(raw string) Field {
	raw = cleanText(raw)
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		f := notFound()
		if raw != "" {
			f.Raw = ptr(raw)
		}
		return f
	}
	return Field{Value: ptr(digits.String()), Raw: ptr(raw), Flags: []string{}}
}

var dateRe = regexp.MustCompile(`^(\d{1,2})\s*[/.\-]\s*(\d{1,2})\s*[/.\-]\s*(\d{4})$`)

func parseDate(s string) (time.Time, bool) {
	m := dateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	y, _ := strconv.Atoi(m[3])
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || int(t.Month()) != mo || t.Year() != y {
		return time.Time{}, false
	}
	return t, true
}

func dateField(raw string) (Field, *time.Time) {
	raw = cleanText(raw)
	if raw == "" {
		return notFound(), nil
	}
	t, ok := parseDate(raw)
	if !ok {
		return Field{Raw: ptr(raw), Flags: []string{FlagInvalidDate}}, nil
	}
	return Field{Value: ptr(t.Format("2006-01-02")), Raw: ptr(raw), Flags: []string{}}, &t
}

func isNoExpiry(raw string) bool {
	l := strings.ToLower(raw)
	return strings.Contains(l, "không thời hạn") || strings.Contains(l, "khong thoi han")
}

func expiryField(raw string) (Field, *time.Time, bool) {
	raw = cleanText(raw)
	if raw != "" && isNoExpiry(raw) {
		return Field{Raw: ptr(raw), Flags: []string{FlagNoExpiry}}, nil, true
	}
	f, t := dateField(raw)
	return f, t, false
}

func sexField(raw string) Field {
	raw = cleanText(raw)
	switch strings.ToLower(raw) {
	case "":
		return notFound()
	case "nam", "male", "m":
		return Field{Value: ptr("M"), Raw: ptr(raw), Flags: []string{}}
	case "nữ", "nu", "female", "f":
		return Field{Value: ptr("F"), Raw: ptr(raw), Flags: []string{}}
	}
	return Field{Raw: ptr(raw), Flags: []string{FlagInvalidFormat}}
}

func normalizeCardVersion(raw string) string {
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case CardChip2021, CardCanCuoc2024, CardBarcode, cardNotCCCD:
		return v
	}
	return CardUnknown
}
