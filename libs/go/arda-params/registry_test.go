package params

import (
	"strings"
	"testing"
)

func TestRenderGoConstantsFromDeclaration(t *testing.T) {
	got, err := RenderGoConstants("loanparams", []CodeSetSpec{{Code: "debt-group", Items: []CodeItemSpec{{Code: "GROUP_1"}, {Code: "GROUP_2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"package loanparams", "DebtGroupGROUP1 = \"GROUP_1\"", "DebtGroupGROUP2 = \"GROUP_2\""} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("generated constants missing %q:\n%s", want, got)
		}
	}
}
