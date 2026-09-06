package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
var newID = repository.NewID

// CatalogDef describes one simple master-data catalog: its storage table,
// id prefix, and how `attributes` (jsonb) are normalized and validated.
type CatalogDef struct {
	Name     string
	Table    string
	IDPrefix string
	Attrs    map[string]attrSpec
}

type attrSpec struct {
	kind     string // "string" | "number" | "int" | "bool"
	required bool
	def      any
	min      *float64
	max      *float64
	enum     []string
}

// Catalogs is the mdm registry. Adding a catalog = one entry here (+ seed
// rows in a migration); storage and HTTP wiring are generic.
var Catalogs = map[string]CatalogDef{
	"currencies": {
		Name: "currencies", Table: "mdm_currencies", IDPrefix: "cur",
		Attrs: map[string]attrSpec{
			"symbol":         {kind: "string", required: true},
			"decimal_places": {kind: "int", min: num(0), max: num(8), def: 2},
		},
	},
	"countries": {
		Name: "countries", Table: "mdm_countries", IDPrefix: "ctry",
		Attrs: map[string]attrSpec{
			"nationality": {kind: "string"},
		},
	},
	"id-document-types": {
		Name: "id-document-types", Table: "mdm_id_document_types", IDPrefix: "iddoc",
		Attrs: map[string]attrSpec{},
	},
	"collateral-types": {
		Name: "collateral-types", Table: "mdm_collateral_types", IDPrefix: "coll",
		Attrs: map[string]attrSpec{
			"group_code":     {kind: "string"},
			"risk_ratio":     {kind: "number", min: num(0), max: num(1), def: 0},
			"deduction_ratio": {kind: "number", min: num(0), max: num(1), def: 0},
			"guarantee_ratio": {kind: "number", min: num(0), max: num(10), def: 0},
		},
	},
	"loan-purposes": {
		Name: "loan-purposes", Table: "mdm_loan_purposes", IDPrefix: "purp",
		Attrs: map[string]attrSpec{
			"risk_level": {kind: "string", enum: []string{"low", "medium", "high"}},
			"limit_min":  {kind: "number", min: num(0)},
			"limit_max":  {kind: "number", min: num(0)},
		},
	},
	"fee-types": {
		Name: "fee-types", Table: "mdm_fee_types", IDPrefix: "fee",
		Attrs: map[string]attrSpec{
			"calc_type":     {kind: "string", required: true, enum: []string{"fixed", "percent"}},
			"default_value": {kind: "number", min: num(0), def: 0},
			"currency_code": {kind: "string"},
		},
	},
	"debt-groups": {
		// Debt classification per Thông tư 02/2023/TT-NHNN (groups 1-5 seeded).
		Name: "debt-groups", Table: "mdm_debt_groups", IDPrefix: "debt",
		Attrs: map[string]attrSpec{
			"provisioning_percent": {kind: "number", required: true, min: num(0), max: num(100)},
			"allows_restructure":   {kind: "bool", def: false},
			"sort_order":           {kind: "int", min: num(0), def: 0},
		},
	},
	// ── EPAS parity batch 2 (2026-09-06): catalogs still missing vs
	// master-data-catalog.md groups 1-3. See docs/epas-survey/mdm-gap-check.md.
	"economic-types": {
		// CtgInfEconomicType — phân loại sở hữu theo Tổng cục Thống kê.
		Name: "economic-types", Table: "mdm_economic_types", IDPrefix: "ect",
		Attrs: map[string]attrSpec{},
	},
	"industries": {
		// CtgInfIndustry — mã ngành nghề kinh doanh.
		Name: "industries", Table: "mdm_industries", IDPrefix: "ind",
		Attrs: map[string]attrSpec{},
	},
	"loan-methods": {
		// LnmCfgLoanMethod — phương thức cho vay.
		Name: "loan-methods", Table: "mdm_loan_methods", IDPrefix: "lmt",
		Attrs: map[string]attrSpec{},
	},
	"loan-contract-types": {
		// LnmCfgContractType — loại hợp đồng tín dụng.
		Name: "loan-contract-types", Table: "mdm_loan_contract_types", IDPrefix: "lct",
		Attrs: map[string]attrSpec{
			"is_collateral": {kind: "bool", def: false},
		},
	},
	"fund-sources": {
		// CfmCfgFundSource/FundType — loại nguồn huy động (CFM P2 dùng).
		Name: "fund-sources", Table: "mdm_fund_sources", IDPrefix: "fsc",
		Attrs: map[string]attrSpec{},
	},
	"fund-purposes": {
		// CfmCfgFundPurpose — mục đích sử dụng vốn.
		Name: "fund-purposes", Table: "mdm_fund_purposes", IDPrefix: "fpu",
		Attrs: map[string]attrSpec{},
	},
	"base-rates": {
		// ComCfgBaseRate — lãi suất tham chiếu thị trường/NHNN theo nguồn.
		Name: "base-rates", Table: "mdm_base_rates", IDPrefix: "brate",
		Attrs: map[string]attrSpec{
			"currency_code": {kind: "string", required: true},
			"source_code":   {kind: "string"},
		},
	},
	"interest-factors": {
		// ComCfgBaseInt — quy ước tử/mẫu số ngày tính lãi (360/365...).
		Name: "interest-factors", Table: "mdm_interest_factors", IDPrefix: "ifact",
		Attrs: map[string]attrSpec{
			"numerator":   {kind: "int", required: true, min: num(1), max: num(400)},
			"denominator": {kind: "int", required: true, min: num(1), max: num(400)},
		},
	},
	"cash-denominations": {
		// VcmCfgCashDenom — mệnh giá tiền theo chất liệu (kho quỹ VCM P2 dùng).
		Name: "cash-denominations", Table: "mdm_cash_denominations", IDPrefix: "denom",
		Attrs: map[string]attrSpec{
			"currency_code": {kind: "string", required: true},
			"value":         {kind: "number", required: true, min: num(0)},
			"material":      {kind: "string", enum: []string{"paper", "polymer", "metal"}},
		},
	},
	"scoring-types": {
		// CtgCfgScoringType — bộ chấm điểm (catalog header); công thức SQL của
		// EPAS bị bỏ chủ ý, thay bằng indicators + benchmarks cấu hình hoá.
		Name: "scoring-types", Table: "mdm_scoring_types", IDPrefix: "score",
		Attrs: map[string]attrSpec{
			"purpose": {kind: "string", enum: []string{"customer", "collateral", "report"}},
		},
	},
	"scoring-indicator-groups": {
		// CtgCfgScoringIndcGroup — nhóm chỉ tiêu trong một bộ chấm điểm.
		Name: "scoring-indicator-groups", Table: "mdm_scoring_indicator_groups", IDPrefix: "sigr",
		Attrs: map[string]attrSpec{
			"scoring_type_code": {kind: "string", required: true},
			"weight":            {kind: "number", min: num(0), max: num(1), def: 0},
		},
	},
	"scoring-indicators": {
		// CtgCfgScoringIndc(IndcMapp) — chỉ tiêu đơn lẻ gắn nhóm + bộ điểm.
		Name: "scoring-indicators", Table: "mdm_scoring_indicators", IDPrefix: "sind",
		Attrs: map[string]attrSpec{
			"scoring_type_code": {kind: "string", required: true},
			"group_code":        {kind: "string"},
			"weight":            {kind: "number", min: num(0), max: num(1), def: 0},
			"data_type":         {kind: "string", enum: []string{"numeric", "enum", "boolean", "text"}},
		},
	},
	"scoring-benchmarks": {
		// CtgCfgScoringBenchmark — thang điểm quy đổi; CONDITION_EXPRESSION
		// dạng SQL của EPAS bị bỏ, chỉ giữ dải điểm số cấu hình hoá.
		Name: "scoring-benchmarks", Table: "mdm_scoring_benchmarks", IDPrefix: "sbn",
		Attrs: map[string]attrSpec{
			"scoring_type_code": {kind: "string", required: true},
			"benchmark_value":   {kind: "number"},
			"score_min":         {kind: "number"},
			"score_max":         {kind: "number"},
		},
	},
}

// CatalogNames returns registry keys in stable order for routing.
func CatalogNames() []string {
	names := make([]string, 0, len(Catalogs))
	for name := range Catalogs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type MasterService struct {
	repo *repository.CatalogRepository
}

func NewMasterService(repo *repository.CatalogRepository) *MasterService {
	return &MasterService{repo: repo}
}

func (s *MasterService) List(ctx context.Context, catalog, tenantID, q string, includeInactive bool) ([]domain.CatalogItem, error) {
	def, err := resolveCatalog(catalog)
	if err != nil {
		return nil, err
	}
	return s.repo.List(ctx, def.Table, tenantID, q, includeInactive)
}

func (s *MasterService) Get(ctx context.Context, catalog, tenantID, id string) (domain.CatalogItem, error) {
	def, err := resolveCatalog(catalog)
	if err != nil {
		return domain.CatalogItem{}, err
	}
	item, err := s.repo.Get(ctx, def.Table, tenantID, id)
	return item, mapRepoError(err)
}

func (s *MasterService) Create(ctx context.Context, catalog, tenantID string, item domain.CatalogItem) (domain.CatalogItem, error) {
	def, err := resolveCatalog(catalog)
	if err != nil {
		return domain.CatalogItem{}, err
	}
	if err := validateCatalogItem(def, &item); err != nil {
		return domain.CatalogItem{}, err
	}
	item.ID = newID(def.IDPrefix)
	if tenantID != "" {
		item.TenantID = &tenantID
	}
	created, err := s.repo.Create(ctx, def.Table, item)
	return created, mapRepoError(err)
}

func (s *MasterService) Update(ctx context.Context, catalog, tenantID, id string, item domain.CatalogItem) (domain.CatalogItem, error) {
	def, err := resolveCatalog(catalog)
	if err != nil {
		return domain.CatalogItem{}, err
	}
	if err := validateCatalogItem(def, &item); err != nil {
		return domain.CatalogItem{}, err
	}
	item.ID = id
	// Only tenant-owned rows are mutable through the API; global seeds are
	// configuration and change via migration, not runtime writes.
	item.TenantID = &tenantID
	updated, err := s.repo.Update(ctx, def.Table, item)
	return updated, mapRepoError(err)
}

func (s *MasterService) Delete(ctx context.Context, catalog, tenantID, id string) error {
	def, err := resolveCatalog(catalog)
	if err != nil {
		return err
	}
	return mapRepoError(s.repo.Delete(ctx, def.Table, tenantID, id))
}

func resolveCatalog(name string) (CatalogDef, error) {
	def, ok := Catalogs[name]
	if !ok {
		return CatalogDef{}, ardaerrors.New(ardaerrors.CodeNotFound, fmt.Sprintf("unknown catalog %q", name))
	}
	return def, nil
}

func mapRepoError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ardaerrors.New(ardaerrors.CodeNotFound, "catalog item not found")
	case errors.Is(err, repository.ErrConflict):
		return ardaerrors.New(ardaerrors.CodeConflict, "catalog code already exists")
	default:
		return ardaerrors.Wrap(ardaerrors.CodeInternal, "catalog operation failed", err)
	}
}

func validateCatalogItem(def CatalogDef, item *domain.CatalogItem) error {
	if !codePattern.MatchString(item.Code) {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "code must match [A-Za-z0-9][A-Za-z0-9_-]{0,31}")
	}
	if strings.TrimSpace(item.Name) == "" {
		return ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	}
	normalized, err := normalizeAttrs(def, item.Attributes)
	if err != nil {
		return err
	}
	item.Attributes = normalized
	return nil
}

// normalizeAttrs validates declared keys and fills defaults; unknown keys are
// rejected so catalog attribute shapes stay auditable.
func normalizeAttrs(def CatalogDef, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidJSON, "attributes must be a JSON object")
	}
	out := make(map[string]any, len(def.Attrs))
	for key, spec := range def.Attrs {
		value, present := attrs[key]
		if !present || value == nil {
			if spec.required {
				return nil, ardaerrors.New(ardaerrors.CodeRequired, fmt.Sprintf("attributes.%s is required", key))
			}
			if spec.def != nil {
				out[key] = spec.def
			}
			continue
		}
		normalized, err := coerceAttr(key, spec, value)
		if err != nil {
			return nil, err
		}
		out[key] = normalized
	}
	for key := range attrs {
		if _, declared := def.Attrs[key]; !declared {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("attributes.%s is not a declared field", key))
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return json.Marshal(out)
}

func coerceAttr(key string, spec attrSpec, value any) (any, error) {
	bad := func(msg string) error {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("attributes.%s %s", key, msg))
	}
	switch spec.kind {
	case "string":
		s, ok := value.(string)
		if !ok {
			return nil, bad("must be a string")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, bad("must not be empty")
		}
		if len(spec.enum) > 0 && !contains(spec.enum, s) {
			return nil, bad(fmt.Sprintf("must be one of %s", strings.Join(spec.enum, "|")))
		}
		return s, nil
	case "number", "int":
		n, ok := toFloat(value)
		if !ok {
			return nil, bad("must be a number")
		}
		if spec.min != nil && n < *spec.min {
			return nil, bad(fmt.Sprintf("must be >= %v", *spec.min))
		}
		if spec.max != nil && n > *spec.max {
			return nil, bad(fmt.Sprintf("must be <= %v", *spec.max))
		}
		if spec.kind == "int" {
			return int64(n), nil
		}
		return n, nil
	case "bool":
		b, ok := value.(bool)
		if !ok {
			return nil, bad("must be a boolean")
		}
		return b, nil
	default:
		return nil, bad("has unsupported spec")
	}
}

func toFloat(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func num(v float64) *float64 { return &v }
