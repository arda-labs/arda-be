package resultid

import (
	"strings"
	"testing"
)

func TestMintProducesValidIDs(t *testing.T) {
	id := Mint("0b5f2c1e-1111-4222-8333-444455556666|9a8b7c6d-1111-4222-8333-444455556666|thread-9_x", 0xabc)
	if id != "0b5f2c1e-1111-4222-8333-444455556666|9a8b7c6d-1111-4222-8333-444455556666|thread-9_x:abc" {
		t.Fatalf("id = %q", id)
	}
	if !Valid(id) {
		t.Fatalf("minted id rejected: %q", id)
	}
	if !Valid(Mint("run-1", 1)) || !Valid(Mint("t1|u1|anonymous", ^uint64(0))) {
		t.Fatal("simple / max-seq ids must be accepted")
	}
}

func TestValidRejectsEscapesAndMalformed(t *testing.T) {
	ns := "t1|u1|th1"
	for name, id := range map[string]string{
		"empty":         "",
		"no suffix":     ns,
		"empty seq":     ns + ":",
		"non-hex seq":   ns + ":xyz",
		"uppercase hex": ns + ":ABC",
		"seq too long":  ns + ":" + strings.Repeat("a", 17),
		"traversal":     "../x?y",
		"slash":         "t1|u1|a/b:1",
		"query":         ns + ":1?x=1",
		"fragment":      ns + ":1#x",
		"percent":       "t1|u1|a%2Fb:1",
		"space":         "t1|u1|a b:1",
		"tab":           "t1|u1|a\tb:1",
		"newline":       ns + ":1\n",
		"control":       "t1|u1|a\x01b:1",
		"del":           "t1|u1|a\x7fb:1",
		"double quote":  "t1|u1|a\"b:1",
		"single quote":  "t1|u1|a'b:1",
		"backslash":     "t1|u1|a\b:1",
		"non-ascii":     "t1|u1|th\u00e9:1",
		"leading colon": ":1",
		"too long":      strings.Repeat("a", maxLen) + ":1",
	} {
		if Valid(id) {
			t.Errorf("%s: %q must be rejected", name, id)
		}
	}
	if !Valid(strings.Repeat("a", maxLen-2) + ":1") {
		t.Error("an id exactly at the cap must be accepted")
	}
}
