package ardahttp

import (
	"net/http"
	"net/url"
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
	if values.Get(QueryPerPage) == "" {
		if raw := strings.TrimSpace(values.Get("size")); raw != "" {
			q.PerPage = parsePositiveInt(raw, DefaultPerPage)
		}
	}
	if q.Q == "" {
		q.Q = strings.TrimSpace(values.Get("search"))
	}
	if q.Sort == "" {
		q.Sort = strings.TrimSpace(values.Get("sortField"))
	}
	if values.Get(QueryOrder) == "" {
		if raw := strings.TrimSpace(values.Get("sortOrder")); raw != "" {
			q.Order = normalizeOrder(raw)
		}
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
