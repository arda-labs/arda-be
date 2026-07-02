package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserInfoMarshalIncludesAuthVersion(t *testing.T) {
	data, err := json.Marshal(&UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 18})
	if err != nil {
		t.Fatalf("marshal user info: %v", err)
	}
	if !strings.Contains(string(data), `"authVersion":18`) {
		t.Fatalf("json = %s, want authVersion", data)
	}
}
