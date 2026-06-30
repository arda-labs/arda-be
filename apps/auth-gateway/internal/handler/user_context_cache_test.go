package handler

import (
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
)

func TestUserContextCacheClonesValues(t *testing.T) {
	cache := newUserContextCache(time.Minute)
	original := &iamclient.UserContext{
		UserID:      "user-1",
		Roles:       []string{"admin"},
		Permissions: []string{"iam.user.read"},
	}

	cache.set("subject-1", original)
	original.Roles[0] = "mutated"

	got, ok := cache.get("subject-1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Roles[0] != "admin" {
		t.Fatalf("cached role mutated: %q", got.Roles[0])
	}

	got.Roles[0] = "reader"
	again, ok := cache.get("subject-1")
	if !ok {
		t.Fatal("expected second cache hit")
	}
	if again.Roles[0] != "admin" {
		t.Fatalf("returned role mutated cache: %q", again.Roles[0])
	}
}

func TestUserContextCacheExpires(t *testing.T) {
	cache := newUserContextCache(time.Nanosecond)
	cache.set("subject-1", &iamclient.UserContext{UserID: "user-1"})
	time.Sleep(time.Millisecond)

	if _, ok := cache.get("subject-1"); ok {
		t.Fatal("expected cache miss after expiry")
	}
}
