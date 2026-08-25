package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeTool struct{}

func (fakeTool) Definition() Definition {
	return Definition{Name: "fake.read", Version: 1, Kind: "read", RequiredPermissions: []string{"crm.customer.read"}, Timeout: time.Second}
}

func (fakeTool) Execute(context.Context, Context, json.RawMessage) (Result, error) {
	return Result{Summary: "ok"}, nil
}

func TestRegistryRequiresDeclaredPermission(t *testing.T) {
	registry := NewRegistry(fakeTool{})
	_, _, err := registry.Resolve(Call{Name: "fake.read"}, Context{TenantID: "tenant", ActorUserID: "user"})
	if !errors.Is(err, ErrToolForbidden) {
		t.Fatalf("error = %v, want permission denial", err)
	}
}

func TestRegistryAllowsSuperadmin(t *testing.T) {
	registry := NewRegistry(fakeTool{})
	item, definition, err := registry.Resolve(Call{Name: "fake.read"}, Context{
		TenantID: "tenant", ActorUserID: "user", Permissions: map[string]struct{}{"superadmin": {}},
	})
	if err != nil || item == nil || definition.Name != "fake.read" {
		t.Fatalf("resolve = %v/%v/%#v", err, item, definition)
	}
}
