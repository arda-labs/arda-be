package service

import "testing"

func TestSubmitRequestHashIsDeterministicAndPayloadBound(t *testing.T) {
	variables := map[string]any{"amount": "100", "currency": "VND"}
	one := submitRequestHash("case-1", "actor-1", "submit-1", variables)
	two := submitRequestHash("case-1", "actor-1", "submit-1", map[string]any{
		"currency": "VND",
		"amount":   "100",
	})
	if one != two {
		t.Fatalf("same request produced different hashes: %q != %q", one, two)
	}
	if one == submitRequestHash("case-1", "actor-1", "submit-1", map[string]any{"amount": "101"}) {
		t.Fatal("different request payload reused the same hash")
	}
}
