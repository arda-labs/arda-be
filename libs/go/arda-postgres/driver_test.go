package ardapostgres

import (
	"testing"
)

func TestDriverHelpers(t *testing.T) {
	var s []string
	if got := Driver.NotNil(s); got == nil || len(got) != 0 {
		t.Fatalf("NotNil(nil) = %v, want empty non-nil slice", got)
	}
	full := []string{"a", "b"}
	if got := Driver.NotNil(full); len(got) != 2 || got[0] != "a" {
		t.Fatalf("NotNil(non-nil) = %v, want unchanged", got)
	}
	var nums []int64
	if got := Driver.NotNil(nums); got == nil {
		t.Fatal("NotNil(nil []int64) returned nil, want empty non-nil slice")
	}
	if Driver.Scanner(nil) == nil {
		t.Fatal("Scanner returned nil, want sql.Scanner wrapper")
	}
}
