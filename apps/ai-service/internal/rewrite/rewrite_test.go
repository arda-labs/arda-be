package rewrite

import "testing"

func TestParseVariants(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"plain array", `["chính sách nghỉ phép 2026", "số ngày phép năm"]`, []string{"chính sách nghỉ phép 2026", "số ngày phép năm"}},
		{"code fence", "```json\n[\"a\", \"b\"]\n```", []string{"a", "b"}},
		{"prose around", `Đây là kết quả: ["x", "y"] nhé.`, []string{"x", "y"}},
		{"drops blanks", `["a", "", "  "]`, []string{"a"}},
		{"no array", `không có gì`, nil},
		{"invalid json", `["a",`, nil},
		{"empty array", `[]`, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseVariants(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("parseVariants(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("parseVariants(%q) = %v, want %v", tc.raw, got, tc.want)
				}
			}
		})
	}
}
