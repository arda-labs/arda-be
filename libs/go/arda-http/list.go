package ardahttp

import (
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

const (
	QueryPage    = "page"
	QueryPerPage = "per_page"
	QuerySort    = "sort"
	QueryOrder   = "order"
	QueryQ       = "q"
	QueryView    = "view"

	DefaultPage    = 1
	DefaultPerPage = 20
	MaxPerPage     = 100
	MaxUnpaginated = 500
)

// ListQuery is the standard paginated list query parsed from HTTP params.
type ListQuery struct {
	Page    int
	PerPage int
	Sort    string
	Order   string
	Q       string
	View    string
	All     bool
}

// ParseListQuery reads page, per_page, sort, order, q, view, and all=1.
func ParseListQuery(values url.Values) ListQuery {
	q := ListQuery{
		Page:    parsePositiveInt(values.Get(QueryPage), DefaultPage),
		PerPage: parsePositiveInt(values.Get(QueryPerPage), DefaultPerPage),
		Sort:    strings.TrimSpace(values.Get(QuerySort)),
		Order:   normalizeOrder(values.Get(QueryOrder)),
		Q:       strings.TrimSpace(values.Get(QueryQ)),
		View:    strings.TrimSpace(values.Get(QueryView)),
		All:     values.Get("all") == "1" || values.Get("all") == "true",
	}
	if q.All || q.View == "tree" || q.View == "options" {
		q.PerPage = MaxUnpaginated
		q.Page = 1
	}
	if q.PerPage > MaxPerPage && !q.All && q.View == "" {
		q.PerPage = MaxPerPage
	}
	return q
}

// Offset returns SQL OFFSET for the current page.
func (q ListQuery) Offset() int {
	if q.Page < 1 {
		return 0
	}
	return (q.Page - 1) * q.PerPage
}

// ListResponse is the standard paginated list JSON body.
type ListResponse[T any] struct {
	Items   []T `json:"items"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// NewListResponse builds a list response, using empty slice instead of null.
func NewListResponse[T any](page, perPage, total int, items []T) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	return ListResponse[T]{
		Items:   items,
		Page:    page,
		PerPage: perPage,
		Total:   total,
	}
}

// WriteList writes a 200 list response with correlation headers.
func WriteList[T any](w http.ResponseWriter, r *http.Request, page, perPage, total int, items []T) {
	WriteJSON(w, r, http.StatusOK, NewListResponse(page, perPage, total, items))
}

// WriteUnpagedList writes a list response for lookup/reference endpoints that
// intentionally return the complete result set in one response.
func WriteUnpagedList[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if items == nil {
		items = []T{}
	}
	perPage := len(items)
	if perPage == 0 {
		perPage = 1
	}
	WriteList(w, r, 1, perPage, len(items), items)
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func normalizeOrder(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "desc":
		return "desc"
	default:
		return "asc"
	}
}

// PickSortField returns field if allowed, otherwise fallback.
func PickSortField(field string, allowed map[string]string, fallback string) string {
	field = strings.TrimSpace(field)
	if mapped, ok := allowed[field]; ok {
		return mapped
	}
	if mapped, ok := allowed[fallback]; ok {
		return mapped
	}
	return fallback
}

// PageSlice applies the parsed ListQuery framing to an in-memory slice and
// reports the values required for a list envelope (paged items, total, page,
// per_page). all=1 / tree / options views return the full slice with per_page
// sized to the result, matching ParseListQuery semantics so every service's
// in-memory list endpoint stays consistent without local reimplementation.
func PageSlice[T any](items []T, q ListQuery) ([]T, int, int, int) {
	total := len(items)
	if q.All || q.View != "" {
		perPage := total
		if perPage == 0 {
			perPage = 1
		}
		return items, total, 1, perPage
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	start := q.Offset()
	if total == 0 || start >= total {
		return []T{}, total, page, q.PerPage
	}
	end := start + q.PerPage
	if end > total {
		end = total
	}
	return items[start:end], total, page, q.PerPage
}

// WriteEnvelopeList writes a migrated success-envelope response whose result
// is the standard paginated list shape. Pair with PageSlice for handlers that
// hold the complete result set in memory.
func WriteEnvelopeList[T any](w http.ResponseWriter, r *http.Request, status int, page, perPage, total int, items []T) {
	WriteSuccess(w, r, status, NewListResponse(page, perPage, total, items))
}

// WriteEnvelopeUnpaged writes a migrated success-envelope response for
// lookup/reference endpoints that intentionally return the complete set in one
// response while keeping the standard list shape.
func WriteEnvelopeUnpaged[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if items == nil {
		items = []T{}
	}
	total := len(items)
	perPage := total
	if perPage == 0 {
		perPage = 1
	}
	WriteEnvelopeList(w, r, http.StatusOK, 1, perPage, total, items)
}

// WriteEnvelopeAnyList mirrors WriteEnvelopeUnpaged for the rare adapter
// surfaces whose item type is only known as `any` at compile time; the slice
// length is derived via reflection instead of per-domain type switches.
func WriteEnvelopeAnyList(w http.ResponseWriter, r *http.Request, items any) {
	total := 0
	if items == nil {
		items = []any{}
	} else if value := reflect.ValueOf(items); value.IsValid() &&
		(value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		total = value.Len()
	}
	perPage := total
	if perPage == 0 {
		perPage = 1
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"items":    items,
		"page":     1,
		"per_page": perPage,
		"total":    total,
	})
}
