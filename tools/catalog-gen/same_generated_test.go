package main

import "testing"

func TestSameGeneratedIgnoresCRLFOnly(t *testing.T) {
	fresh := []byte("package catalog\n\nvar x = 1\n")
	if !sameGenerated([]byte("package catalog\r\n\r\nvar x = 1\r\n"), fresh) {
		t.Fatal("a CRLF checkout of identical content must count as up to date")
	}
	if !sameGenerated(fresh, fresh) {
		t.Fatal("identical content must count as up to date")
	}
	if sameGenerated([]byte("package catalog\r\n\r\nvar x = 2\r\n"), fresh) {
		t.Fatal("a content change must still count as stale")
	}
}
