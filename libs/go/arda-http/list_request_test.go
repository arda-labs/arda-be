package ardahttp

import (
	"net/url"
	"testing"
)

var testListSpec = ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     100,
	SortFields:     []string{"name", "created_at"},
	Views:          []string{"tree", "options"},
	AllowAll:       true,
	Filters: map[string]QueryFilterSpec{
		"is_active": BoolFilter(),
		"status":    CSVFilter(3, "new", "approved"),
	},
}

func TestParseListRequest(t *testing.T) {
	values, _ := url.ParseQuery("page=2&per_page=50&q=arda&sort=name&order=desc&is_active=true&status=new,approved")
	request, err := ParseListRequest(values, testListSpec)
	if err != nil {
		t.Fatalf("ParseListRequest() error = %v", err)
	}
	if request.Page != 2 || request.PerPage != 50 || request.Q != "arda" {
		t.Fatalf("ParseListRequest() pagination/search = %+v", request.ListQuery)
	}
	if request.Sort != "name" || request.Order != "desc" {
		t.Fatalf("ParseListRequest() sorting = %+v", request.ListQuery)
	}
	if request.Bool("is_active") == nil || !*request.Bool("is_active") {
		t.Fatal("ParseListRequest() expected is_active=true")
	}
	if got := request.Strings("status"); len(got) != 2 {
		t.Fatalf("ParseListRequest() status = %v", got)
	}
}

func TestParseListRequestRejectsInvalidContractValues(t *testing.T) {
	tests := []string{
		"page=0",
		"per_page=101",
		"sort=password",
		"order=random",
		"view=unknown",
		"is_active=yes",
		"status=deleted",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			values, _ := url.ParseQuery(raw)
			if _, err := ParseListRequest(values, testListSpec); err == nil {
				t.Fatalf("ParseListRequest(%q) expected error", raw)
			}
		})
	}
}

func TestParseListRequestUnpagedView(t *testing.T) {
	values, _ := url.ParseQuery("page=3&per_page=10&view=tree")
	request, err := ParseListRequest(values, testListSpec)
	if err != nil {
		t.Fatalf("ParseListRequest() error = %v", err)
	}
	if request.Page != 1 || request.PerPage != MaxUnpaginated {
		t.Fatalf("ParseListRequest() unpaged = %+v", request.ListQuery)
	}
}
