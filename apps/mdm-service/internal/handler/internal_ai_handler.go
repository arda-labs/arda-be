package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// aiMaxQueryLen caps the free-text search the assistant can submit; it matches
// the q parameter schema in contracts/ai-internal/mdm-v1.json.
const aiMaxQueryLen = 128

// aiListSpec is the AI tool list contract: per_page 1..20 with default 10 and
// sort limited to code/name. all=1 and views are rejected so the assistant
// can never pull unbounded pages.
var aiListSpec = ardahttp.ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     20,
	SortFields:     []string{"code", "name"},
}

// aiCatalogSource / aiRateSource are the slices of MasterService /
// InterestRateService the AI surface needs. The interface form keeps the
// handler testable without a database while the concrete services satisfy
// it as-is.
type aiCatalogSource interface {
	List(ctx context.Context, catalog, tenantID, q string, includeInactive bool) ([]domain.CatalogItem, error)
}

type aiRateSource interface {
	List(ctx context.Context, tenantID, q string, includeInactive bool) ([]domain.InterestRate, error)
}

// aiAttrAllowlist is the attribute-key allowlist per AI-exposed catalog.
// Master-data attributes are validated at write time, but only display-safe
// keys (currencies: symbol/decimal_places, countries: nationality) are
// forwarded to the assistant; anything else is dropped in the handler.
var aiAttrAllowlist = map[string][]string{
	"currencies": {"symbol", "decimal_places"},
	"countries":  {"nationality"},
}

// InternalAIHandler serves the /internal/ai/* surface consumed by ai-service.
// The signed caller assertion is verified by the router's internalAIService
// middleware; the delegated subject (X-Tenant-Id) is re-validated here and
// scoped again in the repository layer, so a tenant can never see another
// tenant's master data.
type InternalAIHandler struct {
	catalogs aiCatalogSource
	rates    aiRateSource
}

func NewInternalAIHandler(catalogs aiCatalogSource, rates aiRateSource) *InternalAIHandler {
	return &InternalAIHandler{catalogs: catalogs, rates: rates}
}

// InternalAIListCurrencies serves GET /internal/ai/currencies for ai-service.
// Catalog names are hard-coded: the AI surface never takes a catalog name as
// an argument.
func (h *InternalAIHandler) InternalAIListCurrencies(w http.ResponseWriter, r *http.Request) {
	h.listAICatalog(w, r, "currencies")
}

// InternalAIListCountries serves GET /internal/ai/countries for ai-service.
func (h *InternalAIHandler) InternalAIListCountries(w http.ResponseWriter, r *http.Request) {
	h.listAICatalog(w, r, "countries")
}

// listAICatalog lists one hard-coded catalog, redacted for the AI SDK.
// include_inactive stays false — the assistant only sees active master data.
func (h *InternalAIHandler) listAICatalog(w http.ResponseWriter, r *http.Request, catalog string) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.catalogs.List(r.Context(), catalog, tenantID, aiQuery(r), false)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	applyListSort(items, listReq.Sort, listReq.Order, catalogSortStringKeys(), nil)
	writeAIList(w, r, toAICatalogItems(items, aiAttrAllowlist[catalog]), listReq)
}

// InternalAIListInterestRates serves GET /internal/ai/interest-rates for
// ai-service. It is a redaction wrapper around the existing rate list logic:
// tenant_id and created_at/updated_at are dropped. The AI surface returns
// rate headers only, never tiers — tier rows carry decision_no/decision_date
// audit fields and the list endpoint does not join them, so exposing a
// redacted tier shape would widen the surface for no gain.
func (h *InternalAIHandler) InternalAIListInterestRates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, err := h.rates.List(r.Context(), tenantID, aiQuery(r), false)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	applyListSort(items, listReq.Sort, listReq.Order, interestRateSortStringKeys(), nil)
	writeAIList(w, r, toAIInterestRates(items), listReq)
}

// writeAIList pages the redacted slice per the parsed list request and writes
// the canonical success envelope. It mirrors CatalogHandler.List's pipeline
// (parse → fetch → sort → PageSlice) but destructures ardahttp.PageSlice with
// the documented order (paged, total, page, perPage) — the shared listEnvelope
// helper in master_handler.go assigns those returns as (page, perPage, total),
// which the AI surface must not inherit.
func writeAIList[T any](w http.ResponseWriter, r *http.Request, items []T, listReq ardahttp.ListRequest) {
	paged, total, page, perPage := ardahttp.PageSlice(items, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, paged))
}

// aiQuery trims the free-text search and clamps it to aiMaxQueryLen
// (rune-safe, the query can carry Vietnamese text).
func aiQuery(r *http.Request) string {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	runes := []rune(q)
	if len(runes) > aiMaxQueryLen {
		return string(runes[:aiMaxQueryLen])
	}
	return q
}

// aiCatalogEntry is the redacted master-data shape exposed to the AI SDK.
// tenant_id and timestamps are dropped here; the attributes allowlist keeps
// only display-safe keys. The response allowlist in
// contracts/ai-internal/mdm-v1.json drops them again as defense in depth.
type aiCatalogEntry struct {
	ID          string          `json:"id"`
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	IsActive    bool            `json:"is_active"`
	Attributes  json.RawMessage `json:"attributes,omitempty"`
}

func toAICatalogItems(items []domain.CatalogItem, attrKeys []string) []aiCatalogEntry {
	redacted := make([]aiCatalogEntry, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiCatalogEntry{
			ID:          item.ID,
			Code:        item.Code,
			Name:        item.Name,
			Description: item.Description,
			IsActive:    item.IsActive,
			Attributes:  aiSafeAttributes(item.Attributes, attrKeys),
		})
	}
	return redacted
}

// aiSafeAttributes keeps only the allowlisted keys of the stored attributes
// object; unparseable or empty attributes are omitted entirely.
func aiSafeAttributes(raw json.RawMessage, keys []string) json.RawMessage {
	if len(raw) == 0 || len(keys) == 0 {
		return nil
	}
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil
	}
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := attrs[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return encoded
}

// aiInterestRate is the redacted interest-rate header shape. tenant_id and
// created_at/updated_at are dropped; tiers are never returned (see
// InternalAIListInterestRates).
type aiInterestRate struct {
	ID           string  `json:"id"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	RateType     string  `json:"rate_type"`
	ApplyType    string  `json:"apply_type"`
	CurrencyCode *string `json:"currency_code,omitempty"`
	Description  *string `json:"description,omitempty"`
	IsActive     bool    `json:"is_active"`
}

func toAIInterestRates(items []domain.InterestRate) []aiInterestRate {
	redacted := make([]aiInterestRate, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiInterestRate{
			ID:           item.ID,
			Code:         item.Code,
			Name:         item.Name,
			RateType:     item.RateType,
			ApplyType:    item.ApplyType,
			CurrencyCode: item.CurrencyCode,
			Description:  item.Description,
			IsActive:     item.IsActive,
		})
	}
	return redacted
}
