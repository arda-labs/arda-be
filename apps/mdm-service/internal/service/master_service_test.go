package service

import (
	"encoding/json"
	"testing"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

func TestValidateCatalogItemRejectsBadCode(t *testing.T) {
	def := Catalogs["currencies"]
	item := domain.CatalogItem{Code: "vnd!", Name: "VND", Attributes: json.RawMessage(`{"symbol":"₫"}`)}
	err := validateCatalogItem(def, &item)
	if err == nil {
		t.Fatal("expected invalid code to be rejected")
	}
	if appErr, ok := err.(*ardaerrors.Error); !ok || appErr.Code != ardaerrors.CodeInvalidInput {
		t.Fatalf("expected CodeInvalidInput, got %v", err)
	}
}

func TestValidateCatalogItemRequiresName(t *testing.T) {
	def := Catalogs["currencies"]
	item := domain.CatalogItem{Code: "VND", Attributes: json.RawMessage(`{"symbol":"₫"}`)}
	err := validateCatalogItem(def, &item)
	if err == nil {
		t.Fatal("expected missing name to be rejected")
	}
}

func TestNormalizeAttrsFillsDefaults(t *testing.T) {
	def := Catalogs["currencies"]
	attrs, err := normalizeAttrs(def, json.RawMessage(`{"symbol":"$"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(attrs, &out); err != nil {
		t.Fatal(err)
	}
	if out["decimal_places"] != float64(2) {
		t.Fatalf("expected default decimal_places=2, got %v", out["decimal_places"])
	}
}

func TestNormalizeAttrsRejectsUnknownKeys(t *testing.T) {
	def := Catalogs["debt-groups"]
	_, err := normalizeAttrs(def, json.RawMessage(`{"provisioning_percent":50,"mystery":1}`))
	if err == nil {
		t.Fatal("expected unknown attribute key to be rejected")
	}
}

func TestNormalizeAttrsDebtGroupDefaults(t *testing.T) {
	def := Catalogs["debt-groups"]
	attrs, err := normalizeAttrs(def, json.RawMessage(`{"provisioning_percent":20}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(attrs, &out)
	if out["allows_restructure"] != false {
		t.Fatalf("expected default allows_restructure=false, got %v", out["allows_restructure"])
	}
	if out["sort_order"] != float64(0) {
		t.Fatalf("expected default sort_order=0, got %v", out["sort_order"])
	}
}

func TestNormalizeAttrsEnumValidation(t *testing.T) {
	def := Catalogs["fee-types"]
	if _, err := normalizeAttrs(def, json.RawMessage(`{"calc_type":"bogus"}`)); err == nil {
		t.Fatal("expected invalid enum value to be rejected")
	}
	attrs, err := normalizeAttrs(def, json.RawMessage(`{"calc_type":"percent","default_value":1.5}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(attrs, &out)
	if out["calc_type"] != "percent" {
		t.Fatalf("expected calc_type=percent, got %v", out["calc_type"])
	}
}

func TestValidateTierDateWindow(t *testing.T) {
	tier := domain.InterestRateTier{EffectiveFrom: "2026-01-01", EffectiveTo: strPtr("2025-01-01"), RateValue: 8}
	if err := validateTier(&tier); err == nil {
		t.Fatal("expected effective_to before effective_from to be rejected")
	}
	tier.EffectiveTo = strPtr("2026-12-31")
	if err := validateTier(&tier); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTierBadDate(t *testing.T) {
	tier := domain.InterestRateTier{EffectiveFrom: "01-2026-01", RateValue: 8}
	if err := validateTier(&tier); err == nil {
		t.Fatal("expected malformed date to be rejected")
	}
}

func TestCatalogNamesStableOrder(t *testing.T) {
	names := CatalogNames()
	if len(names) != len(Catalogs) {
		t.Fatalf("expected %d catalogs, got %d", len(Catalogs), len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("catalog names not sorted: %v", names)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestLoanContractTypeCollateralDefault(t *testing.T) {
	def := Catalogs["loan-contract-types"]
	attrs, err := normalizeAttrs(def, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(attrs, &out)
	if out["is_collateral"] != false {
		t.Fatalf("expected default is_collateral=false, got %v", out["is_collateral"])
	}
}

func TestInterestFactorRequiresNumDenom(t *testing.T) {
	def := Catalogs["interest-factors"]
	if _, err := normalizeAttrs(def, json.RawMessage(`{"numerator":365}`)); err == nil {
		t.Fatal("expected missing denominator to be rejected")
	}
	if _, err := normalizeAttrs(def, json.RawMessage(`{"numerator":365,"denominator":900}`)); err == nil {
		t.Fatal("expected denominator out of range to be rejected")
	}
	attrs, err := normalizeAttrs(def, json.RawMessage(`{"numerator":365,"denominator":360}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(attrs, &out)
	if out["numerator"] != float64(365) || out["denominator"] != float64(360) {
		t.Fatalf("unexpected factors: %v", out)
	}
}

func TestCashDenominationMaterialEnum(t *testing.T) {
	def := Catalogs["cash-denominations"]
	if _, err := normalizeAttrs(def, json.RawMessage(`{"currency_code":"VND","value":500000,"material":"wood"}`)); err == nil {
		t.Fatal("expected invalid material to be rejected")
	}
	attrs, err := normalizeAttrs(def, json.RawMessage(`{"currency_code":"VND","value":500000}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(attrs, &out)
	if _, present := out["material"]; present {
		t.Fatalf("expected no default material, got %v", out["material"])
	}
}

func TestScoringIndicatorDefaults(t *testing.T) {
	def := Catalogs["scoring-indicators"]
	if _, err := normalizeAttrs(def, json.RawMessage(`{"weight":0.3}`)); err == nil {
		t.Fatal("expected missing scoring_type_code to be rejected")
	}
	attrs, err := normalizeAttrs(def, json.RawMessage(`{"scoring_type_code":"CIF_CORP"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(attrs, &out)
	if out["weight"] != float64(0) {
		t.Fatalf("expected default weight=0, got %v", out["weight"])
	}
	if _, present := out["data_type"]; present {
		t.Fatalf("expected no default data_type, got %v", out["data_type"])
	}
}

func TestBaseRateRequiresCurrency(t *testing.T) {
	def := Catalogs["base-rates"]
	if _, err := normalizeAttrs(def, json.RawMessage(`{"source_code":"NHNN"}`)); err == nil {
		t.Fatal("expected missing currency_code to be rejected")
	}
}
