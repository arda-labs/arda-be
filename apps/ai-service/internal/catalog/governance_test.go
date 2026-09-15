package catalog

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type fakeToolSettingsStore struct {
	mu        sync.Mutex
	items     map[string]repository.ToolSetting
	listCalls int
}

func newFakeToolSettingsStore() *fakeToolSettingsStore {
	return &fakeToolSettingsStore{items: map[string]repository.ToolSetting{}}
}

func (f *fakeToolSettingsStore) ListToolSettings(context.Context) ([]repository.ToolSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	out := make([]repository.ToolSetting, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, item)
	}
	return out, nil
}

func (f *fakeToolSettingsStore) UpsertToolSetting(_ context.Context, methodName string, enabled bool, updatedBy string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.items[methodName] = repository.ToolSetting{MethodName: methodName, Enabled: enabled, UpdatedBy: updatedBy, UpdatedAt: now}
	return now, nil
}

func (f *fakeToolSettingsStore) DeleteToolSetting(_ context.Context, methodName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, methodName)
	return nil
}

func testEntry(methodName string, enabled bool) CatalogEntry {
	return CatalogEntry{
		MethodName:          methodName,
		SDKPath:             "arda." + methodName,
		Domain:              "test",
		Kind:                "read",
		RequiredPermissions: []string{"ai.assistant.use"},
		Risk:                "low",
		Enabled:             enabled,
	}
}

// The effective state follows ADR-003 §2:
// effectiveEnabled = contractEnabled && (overrideEnabled ?? true).
func TestGovernanceEffectiveFormula(t *testing.T) {
	store := newFakeToolSettingsStore()
	gov := NewGovernance(store)
	entry := testEntry("crm.getCustomer", true)

	if !gov.IsEnabled(entry) {
		t.Fatal("contract-enabled entry without override must be enabled")
	}

	if _, err := gov.SetOverride(context.Background(), entry.MethodName, false, "admin"); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if gov.IsEnabled(entry) {
		t.Fatal("override false must disable the entry")
	}
	if override, ok := gov.Override(entry.MethodName); !ok || override {
		t.Fatalf("override = %v, %v; want false, true", override, ok)
	}

	if _, err := gov.SetOverride(context.Background(), entry.MethodName, true, "admin"); err != nil {
		t.Fatalf("set override true: %v", err)
	}
	if !gov.IsEnabled(entry) {
		t.Fatal("override true must re-enable a contract-enabled entry")
	}

	if _, err := gov.ClearOverride(context.Background(), entry.MethodName, "admin"); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if _, ok := gov.Override(entry.MethodName); ok {
		t.Fatal("clear must remove the override")
	}
	if !gov.IsEnabled(entry) {
		t.Fatal("cleared override must return to the contract default")
	}
}

// A contract-disabled entry is a hard floor: the runtime override cannot lift
// it, even if a row with enabled=true exists in the store.
func TestGovernanceContractFloor(t *testing.T) {
	store := newFakeToolSettingsStore()
	store.items["crm.exportCustomer"] = repository.ToolSetting{MethodName: "crm.exportCustomer", Enabled: true, UpdatedBy: "admin"}
	gov := NewGovernance(store)
	if err := gov.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("ensure fresh: %v", err)
	}

	entry := testEntry("crm.exportCustomer", false)
	if gov.IsEnabled(entry) {
		t.Fatal("contract-disabled entry must stay disabled regardless of override")
	}
}

func TestGovernanceUnavailableWithoutStore(t *testing.T) {
	gov := NewGovernance(nil)
	if gov.Available() {
		t.Fatal("governance without a store must report unavailable")
	}
	if err := gov.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("ensure fresh without store: %v", err)
	}
	if !gov.IsEnabled(testEntry("crm.getCustomer", true)) {
		t.Fatal("nil-store governance must follow the contract default")
	}
	if _, err := gov.SetOverride(context.Background(), "crm.getCustomer", false, "admin"); err == nil {
		t.Fatal("set override without a store must fail")
	}
}

func TestGovernanceEnsuresFreshWithinTTL(t *testing.T) {
	store := newFakeToolSettingsStore()
	gov := NewGovernance(store)
	if err := gov.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if err := gov.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if store.listCalls != 1 {
		t.Fatalf("list calls = %d, want 1 within the TTL", store.listCalls)
	}
}

func TestRegistrySkipsDisabledEntries(t *testing.T) {
	store := newFakeToolSettingsStore()
	gov := NewGovernance(store)
	reg := NewDispatcherRegistry()
	reg.Register(testEntry("crm.getCustomer", true), func(context.Context, tools.Context, map[string]any) (any, error) {
		return nil, nil
	})
	reg.Register(testEntry("hrm.listEmployees", true), func(context.Context, tools.Context, map[string]any) (any, error) {
		return nil, nil
	})
	reg.SetEnabledPredicate(gov.IsEnabled)

	if _, _, ok := reg.Resolve("crm.getCustomer"); !ok {
		t.Fatal("enabled entry must resolve")
	}
	if len(reg.AllSDKMethods()) != 2 {
		t.Fatalf("sdk methods = %d, want 2", len(reg.AllSDKMethods()))
	}

	if _, err := gov.SetOverride(context.Background(), "crm.getCustomer", false, "admin"); err != nil {
		t.Fatalf("set override: %v", err)
	}

	if _, _, ok := reg.Resolve("crm.getCustomer"); ok {
		t.Fatal("disabled entry must not resolve")
	}
	if len(reg.AllSDKMethods()) != 1 {
		t.Fatalf("sdk methods = %d, want 1 after disabling", len(reg.AllSDKMethods()))
	}
	if entries := reg.EnabledEntries(); len(entries) != 1 || entries[0].MethodName != "hrm.listEmployees" {
		t.Fatalf("enabled entries = %+v, want only hrm.listEmployees", entries)
	}
	if entries := reg.AllEntries(); len(entries) != 2 {
		t.Fatalf("all entries = %d, want 2 (admin inventory keeps disabled tools)", len(entries))
	}
}

func TestIndexFilterExcludesDisabledEntries(t *testing.T) {
	idx := NewIndex([]CatalogEntry{
		testEntry("crm.getCustomer", true),
		testEntry("hrm.listEmployees", true),
	})
	idx.SetFilter(func(entry CatalogEntry) bool { return entry.MethodName != "crm.getCustomer" })

	scope := tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		Permissions: map[string]struct{}{"ai.assistant.use": {}},
	}
	results := idx.Search("getCustomer", "", scope, 5)
	for _, entry := range results {
		if entry.MethodName == "crm.getCustomer" {
			t.Fatal("disabled entry leaked into search results")
		}
	}
}

// Every builtin entry must opt in explicitly: CatalogEntry.Enabled is a
// contract default, so a forgotten literal would silently disable the tool.
func TestBuiltinCatalogEntriesAreEnabled(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))

	entries := reg.AllEntries()
	if len(entries) == 0 {
		t.Fatal("no catalog entries registered")
	}
	for _, entry := range entries {
		if !entry.Enabled {
			t.Errorf("entry %s is not contract-enabled; set Enabled: true explicitly", entry.MethodName)
		}
	}
}
