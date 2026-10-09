package knowledge

import "testing"

func TestBuildFTSQuery(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"   !!! ":                 "",
		"Quy trình nghỉ phép":     "quy | trình | nghỉ | phép",
		"nghỉ phép, nghỉ PHÉP?":   "nghỉ | phép",
		"a b c đi":                "đi",
		"x'; DROP TABLE y; --":    "drop | table",
		"hạn mức & (tín | dụng)!": "hạn | mức | tín | dụng",
		"2026 báo cáo Q3/2026":    "2026 | báo | cáo | q3",
	}
	for input, want := range cases {
		if got := buildFTSQuery(input); got != want {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildFTSQueryBoundsTermCount(t *testing.T) {
	input := "aa bb cc dd ee ff gg hh ii jj kk ll mm nn oo"
	if got := buildFTSQuery(input); got != "aa | bb | cc | dd | ee | ff | gg | hh | ii | jj | kk | ll" {
		t.Fatalf("expected the first 12 terms, got %q", got)
	}
}
