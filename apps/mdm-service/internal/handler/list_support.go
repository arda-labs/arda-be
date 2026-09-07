package handler

import (
	"sort"
	"time"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
)

// The mdm repositories return the full matching set and the handlers frame
// it with ardahttp.PageSlice, so the is_active filter and the sort whitelist
// are applied in memory here — equivalent to SQL WHERE / ORDER BY because
// both run over the complete result before paging.

// isActiveSelection reports whether the client narrowed is_active to exactly
// one boolean value ("true" or "false").
func isActiveSelection(selected []string) (wantActive, narrowed bool) {
	values := make(map[string]bool, len(selected))
	for _, value := range selected {
		values[value] = true
	}
	if len(values) != 1 {
		return false, false
	}
	_, wantActive = values["true"]
	return wantActive, true
}

// filterByActive keeps only the rows whose is_active matches the single
// selected value; with no (or both) selections every row stays.
func filterByActive[T any](items []T, selected []string, isActive func(T) bool) []T {
	wantActive, narrowed := isActiveSelection(selected)
	if !narrowed {
		return items
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		if isActive(item) == wantActive {
			out = append(out, item)
		}
	}
	return out
}

// applyListSort orders items by the validated sort whitelist key; unknown
// keys keep the repository default order (code). SliceStable preserves that
// default as tie-breaker for equal keys.
func applyListSort[T any](
	items []T,
	sortKey string,
	order string,
	stringKeys map[string]func(T) string,
	timeKeys map[string]func(T) time.Time,
) {
	desc := order == "desc"
	if get, ok := stringKeys[sortKey]; ok {
		sort.SliceStable(items, func(i, j int) bool {
			if desc {
				return get(items[j]) < get(items[i])
			}
			return get(items[i]) < get(items[j])
		})
		return
	}
	if get, ok := timeKeys[sortKey]; ok {
		sort.SliceStable(items, func(i, j int) bool {
			if desc {
				return get(items[j]).Before(get(items[i]))
			}
			return get(items[i]).Before(get(items[j]))
		})
	}
}

func catalogSortStringKeys() map[string]func(domain.CatalogItem) string {
	return map[string]func(domain.CatalogItem) string{
		"code": func(item domain.CatalogItem) string { return item.Code },
		"name": func(item domain.CatalogItem) string { return item.Name },
	}
}

func catalogSortTimeKeys() map[string]func(domain.CatalogItem) time.Time {
	return map[string]func(domain.CatalogItem) time.Time{
		"created_at": func(item domain.CatalogItem) time.Time { return item.CreatedAt },
	}
}

func interestRateSortStringKeys() map[string]func(domain.InterestRate) string {
	return map[string]func(domain.InterestRate) string{
		"code": func(item domain.InterestRate) string { return item.Code },
		"name": func(item domain.InterestRate) string { return item.Name },
	}
}

func interestRateSortTimeKeys() map[string]func(domain.InterestRate) time.Time {
	return map[string]func(domain.InterestRate) time.Time{
		"created_at": func(item domain.InterestRate) time.Time { return item.CreatedAt },
	}
}
