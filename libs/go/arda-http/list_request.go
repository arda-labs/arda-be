package ardahttp

import (
	"net/url"
	"strconv"
	"strings"
)

type QueryFilterKind string

const (
	QueryFilterBool QueryFilterKind = "bool"
	QueryFilterEnum QueryFilterKind = "enum"
	QueryFilterCSV  QueryFilterKind = "csv"
)

type QueryFilterSpec struct {
	Kind          QueryFilterKind
	AllowedValues []string
	MaxItems      int
}

func BoolFilter() QueryFilterSpec {
	return QueryFilterSpec{Kind: QueryFilterBool}
}

func EnumFilter(allowed ...string) QueryFilterSpec {
	return QueryFilterSpec{Kind: QueryFilterEnum, AllowedValues: allowed}
}

func CSVFilter(maxItems int, allowed ...string) QueryFilterSpec {
	return QueryFilterSpec{
		Kind:          QueryFilterCSV,
		AllowedValues: allowed,
		MaxItems:      maxItems,
	}
}

// ListSpec declares the accepted public query contract for one list endpoint.
type ListSpec struct {
	DefaultPerPage int
	MaxPerPage     int
	SortFields     []string
	Views          []string
	AllowAll       bool
	Filters        map[string]QueryFilterSpec
}

type ListRequest struct {
	ListQuery
	filters map[string]any
}

func (r ListRequest) Bool(key string) *bool {
	value, _ := r.filters[key].(*bool)
	return value
}

func (r ListRequest) String(key string) string {
	value, _ := r.filters[key].(string)
	return value
}

func (r ListRequest) Strings(key string) []string {
	value, _ := r.filters[key].([]string)
	return append([]string(nil), value...)
}

// ParseListRequest validates pagination, sorting, views and endpoint filters.
func ParseListRequest(values url.Values, spec ListSpec) (ListRequest, error) {
	defaultPerPage := spec.DefaultPerPage
	if defaultPerPage < 1 {
		defaultPerPage = DefaultPerPage
	}
	maxPerPage := spec.MaxPerPage
	if maxPerPage < 1 {
		maxPerPage = MaxPerPage
	}

	page, err := parsePositiveQueryInt(values, QueryPage, DefaultPage, 0)
	if err != nil {
		return ListRequest{}, err
	}
	perPage, err := parsePositiveQueryInt(values, QueryPerPage, defaultPerPage, maxPerPage)
	if err != nil {
		return ListRequest{}, err
	}

	sortField := strings.TrimSpace(values.Get(QuerySort))
	if sortField != "" && !containsString(spec.SortFields, sortField) {
		return ListRequest{}, &QueryValueError{
			Key:      QuerySort,
			Value:    sortField,
			Expected: strings.Join(spec.SortFields, ", "),
		}
	}

	order, err := parseQueryOrder(values.Get(QueryOrder))
	if err != nil {
		return ListRequest{}, err
	}
	view, err := ParseOptionalEnum(values, QueryView, spec.Views...)
	if err != nil {
		return ListRequest{}, err
	}
	all, err := parseListAll(values, spec.AllowAll)
	if err != nil {
		return ListRequest{}, err
	}

	request := ListRequest{
		ListQuery: ListQuery{
			Page:    page,
			PerPage: perPage,
			Sort:    sortField,
			Order:   order,
			Q:       strings.TrimSpace(values.Get(QueryQ)),
			View:    view,
			All:     all,
		},
		filters: make(map[string]any, len(spec.Filters)),
	}
	if request.All || request.View != "" {
		request.Page = 1
		request.PerPage = MaxUnpaginated
	}

	for key, filterSpec := range spec.Filters {
		if err := request.parseFilter(values, key, filterSpec); err != nil {
			return ListRequest{}, err
		}
	}
	return request, nil
}

func (r *ListRequest) parseFilter(values url.Values, key string, spec QueryFilterSpec) error {
	switch spec.Kind {
	case QueryFilterBool:
		value, err := ParseOptionalBool(values, key)
		if err != nil {
			return err
		}
		if value != nil {
			r.filters[key] = value
		}
	case QueryFilterEnum:
		value, err := ParseOptionalEnum(values, key, spec.AllowedValues...)
		if err != nil {
			return err
		}
		if value != "" {
			r.filters[key] = value
		}
	case QueryFilterCSV:
		items := ParseCSVQuery(values, key)
		if spec.MaxItems > 0 && len(items) > spec.MaxItems {
			return &QueryValueError{Key: key, Value: values.Get(key), Expected: "fewer values"}
		}
		for _, item := range items {
			if len(spec.AllowedValues) > 0 && !containsString(spec.AllowedValues, item) {
				return &QueryValueError{
					Key:      key,
					Value:    item,
					Expected: strings.Join(spec.AllowedValues, ", "),
				}
			}
		}
		if len(items) > 0 {
			r.filters[key] = items
		}
	default:
		return &QueryValueError{Key: key, Value: string(spec.Kind), Expected: "known filter kind"}
	}
	return nil
}

func parsePositiveQueryInt(values url.Values, key string, fallback, max int) (int, error) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || (max > 0 && value > max) {
		expected := "a positive integer"
		if max > 0 {
			expected = "an integer between 1 and " + strconv.Itoa(max)
		}
		return 0, &QueryValueError{Key: key, Value: raw, Expected: expected}
	}
	return value, nil
}

func parseQueryOrder(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "asc":
		return "asc", nil
	case "desc":
		return "desc", nil
	default:
		return "", &QueryValueError{Key: QueryOrder, Value: raw, Expected: "asc or desc"}
	}
}

func parseListAll(values url.Values, allowed bool) (bool, error) {
	raw := strings.TrimSpace(values.Get("all"))
	if raw == "" || raw == "0" || raw == "false" {
		return false, nil
	}
	if allowed && (raw == "1" || raw == "true") {
		return true, nil
	}
	return false, &QueryValueError{Key: "all", Value: raw, Expected: "false"}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
