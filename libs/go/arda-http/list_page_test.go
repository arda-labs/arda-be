package ardahttp

import (
	"math"
	"net/url"
	"strconv"
	"testing"
)

func TestParseListQueryClampsHugePage(t *testing.T) {
	values := url.Values{
		QueryPage:    {strconv.Itoa(math.MaxInt)},
		QueryPerPage: {"50"},
	}
	query := ParseListQuery(values)
	if query.Page != MaxPage {
		t.Fatalf("ParseListQuery() page = %d, want %d", query.Page, MaxPage)
	}
	if offset := query.Offset(); offset < 0 {
		t.Fatalf("Offset() = %d, want non-negative", offset)
	}
}

func TestOffsetNeverNegativeOnOverflow(t *testing.T) {
	queries := []ListQuery{
		{Page: math.MaxInt, PerPage: math.MaxInt},
		{Page: math.MaxInt, PerPage: 2},
		{Page: 2, PerPage: math.MaxInt},
		{Page: -5, PerPage: 10},
	}
	for _, query := range queries {
		if offset := query.Offset(); offset < 0 {
			t.Fatalf("Offset(%+v) = %d, want non-negative", query, offset)
		}
	}
}

func TestPageSliceHugePageReturnsEmptyWithoutPanic(t *testing.T) {
	items := []int{1, 2, 3}

	query := ParseListQuery(url.Values{QueryPage: {"99999999"}, QueryPerPage: {"10"}})
	result := PageSlice(items, query)
	if len(result.Items) != 0 || result.Items == nil {
		t.Fatalf("PageSlice() items = %#v, want empty non-nil slice", result.Items)
	}
	if result.Total != 3 || result.Page != MaxPage || result.PerPage != 10 {
		t.Fatalf("PageSlice() = %+v", result)
	}

	// A hand-built query that would overflow (page-1)*per_page must not panic.
	result = PageSlice(items, ListQuery{Page: math.MaxInt, PerPage: math.MaxInt})
	if len(result.Items) != 0 {
		t.Fatalf("PageSlice() overflow items = %v, want empty", result.Items)
	}
	if result.Total != 3 || result.Page != math.MaxInt {
		t.Fatalf("PageSlice() overflow = %+v", result)
	}
}

func TestParseListQueryUnknownViewKeepsPerPageCap(t *testing.T) {
	query := ParseListQuery(url.Values{QueryView: {"abc"}, QueryPerPage: {"100000"}})
	if query.PerPage != MaxPerPage {
		t.Fatalf("ParseListQuery() per_page = %d, want %d", query.PerPage, MaxPerPage)
	}
	if query.Page != DefaultPage {
		t.Fatalf("ParseListQuery() page = %d, want %d", query.Page, DefaultPage)
	}
	if query.View != "abc" {
		t.Fatalf("ParseListQuery() view = %q, want %q", query.View, "abc")
	}

	result := PageSlice([]int{1, 2, 3}, query)
	if result.PerPage != MaxPerPage {
		t.Fatalf("PageSlice() per_page = %d, want %d", result.PerPage, MaxPerPage)
	}
	if len(result.Items) != 3 || result.Page != DefaultPage {
		t.Fatalf("PageSlice() = %+v", result)
	}
}

func TestParseListQueryUnpagedViews(t *testing.T) {
	for _, raw := range []string{"view=tree", "view=options", "all=1"} {
		values, _ := url.ParseQuery(raw)
		query := ParseListQuery(values)
		if query.Page != 1 || query.PerPage != MaxUnpaginated {
			t.Fatalf("ParseListQuery(%q) = %+v, want page=1 per_page=%d", raw, query, MaxUnpaginated)
		}
		result := PageSlice([]int{1, 2, 3}, query)
		if result.Page != 1 || result.PerPage != 3 || len(result.Items) != 3 {
			t.Fatalf("PageSlice(%q) = %+v", raw, result)
		}
	}
}

func TestPageSliceReturnsTypedFields(t *testing.T) {
	items := []int{0, 1, 2, 3, 4, 5, 6}
	result := PageSlice(items, ListQuery{Page: 2, PerPage: 3})
	if result.Total != len(items) || result.Page != 2 || result.PerPage != 3 {
		t.Fatalf("PageSlice() metadata = %+v", result)
	}
	if len(result.Items) != 3 || result.Items[0] != 3 || result.Items[2] != 5 {
		t.Fatalf("PageSlice() items = %v", result.Items)
	}

	empty := PageSlice([]int{}, ListQuery{Page: 1, PerPage: 3})
	if empty.Items == nil || empty.Total != 0 || empty.PerPage != 3 {
		t.Fatalf("PageSlice() empty = %+v", empty)
	}
}
