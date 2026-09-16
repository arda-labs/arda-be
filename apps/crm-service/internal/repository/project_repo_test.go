package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func maxPlaceholder(t *testing.T, query string) int {
	t.Helper()
	max := 0
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(query, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("bad placeholder %q in %q: %v", m[1], query, err)
		}
		if n > max {
			max = n
		}
	}
	return max
}

func TestProjectListWherePlaceholdersMatchArgs(t *testing.T) {
	tests := []struct {
		name     string
		orgCodes []string
		status   string
		q        string
	}{
		{name: "tenant only"},
		{name: "status filter", status: "ACTIVE"},
		{name: "search filter", q: "alpha"},
		{name: "org filter", orgCodes: []string{"ORG1", "ORG2"}},
		{name: "all filters", orgCodes: []string{"ORG1"}, status: "ACTIVE", q: "alpha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := projectListWhere("t1", tt.orgCodes, tt.status, tt.q)
			if !strings.HasPrefix(where, "tenant_id = $1") {
				t.Fatalf("tenant scope must be the first filter, got %q", where)
			}
			if args[0] != "t1" {
				t.Fatalf("first arg = %v, want tenant", args[0])
			}
			if max := maxPlaceholder(t, where); max != len(args) {
				t.Fatalf("max placeholder $%d does not match %d args (where=%q)", max, len(args), where)
			}
		})
	}
}

func TestProjectListWhereOrgFilterOptional(t *testing.T) {
	where, _ := projectListWhere("t1", nil, "", "")
	if strings.Contains(where, "org_code") {
		t.Fatalf("empty org scope must not filter org_code, got %q", where)
	}
	where, args := projectListWhere("t1", []string{"ORG1"}, "", "")
	if !strings.Contains(where, "org_code = ANY($2)") {
		t.Fatalf("org scope must filter org_code, got %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("args = %d, want 2", len(args))
	}
}

func TestProjectListBounds(t *testing.T) {
	tests := []struct {
		name             string
		page, perPage    int
		wantPage, wantPP int
	}{
		{name: "defaults from zero", page: 0, perPage: 0, wantPage: 1, wantPP: projectListMaxPerPage},
		{name: "negative clamped", page: -3, perPage: -5, wantPage: 1, wantPP: projectListMaxPerPage},
		{name: "over cap clamped", page: 2, perPage: 5000, wantPage: 2, wantPP: projectListMaxPerPage},
		{name: "valid kept", page: 3, perPage: 50, wantPage: 3, wantPP: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, perPage := projectListBounds(tt.page, tt.perPage)
			if page != tt.wantPage || perPage != tt.wantPP {
				t.Fatalf("projectListBounds(%d, %d) = (%d, %d), want (%d, %d)",
					tt.page, tt.perPage, page, perPage, tt.wantPage, tt.wantPP)
			}
		})
	}
}

func TestProjectScopeWherePlaceholdersMatchArgs(t *testing.T) {
	tests := []struct {
		name     string
		orgCodes []string
		offset   int
	}{
		{name: "tenant only"},
		{name: "org filter", orgCodes: []string{"ORG1", "ORG2"}},
		{name: "reserved set placeholders", offset: 7},
		{name: "reserved placeholders with org", orgCodes: []string{"ORG1"}, offset: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := projectScopeWhere("t1", "prj-1", tt.orgCodes, tt.offset)
			wantPrefix := fmt.Sprintf("tenant_id = $%d AND id = $%d::uuid", tt.offset+1, tt.offset+2)
			if !strings.HasPrefix(where, wantPrefix) {
				t.Fatalf("where = %q, want prefix %q", where, wantPrefix)
			}
			if args[0] != "t1" || args[1] != "prj-1" {
				t.Fatalf("args = %v, want tenant then project id", args)
			}
			if len(tt.orgCodes) > 0 {
				wantOrg := fmt.Sprintf("org_code = ANY($%d)", tt.offset+3)
				if !strings.Contains(where, wantOrg) {
					t.Fatalf("where = %q, want %q", where, wantOrg)
				}
				if len(args) != 3 {
					t.Fatalf("args = %d, want 3", len(args))
				}
			} else {
				if strings.Contains(where, "org_code") {
					t.Fatalf("empty org scope must not filter org_code, got %q", where)
				}
				if len(args) != 2 {
					t.Fatalf("args = %d, want 2", len(args))
				}
			}
			if got := maxPlaceholder(t, where); got != tt.offset+len(args) {
				t.Fatalf("max placeholder $%d does not match offset %d + %d args (where=%q)",
					got, tt.offset, len(args), where)
			}
		})
	}
}

func TestCustomerScopeWherePlaceholdersMatchArgs(t *testing.T) {
	where, args := customerScopeWhere("t1", "cust-1", nil, 0)
	if where != "tenant_id = $1 AND id = $2" {
		t.Fatalf("where = %q", where)
	}
	if strings.Contains(where, "uuid") {
		t.Fatalf("customers.id is VARCHAR and must not be cast to uuid: %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("args = %d, want 2", len(args))
	}

	where, args = customerScopeWhere("t1", "cust-1", []string{"ORG1"}, 9)
	if want := "tenant_id = $10 AND id = $11 AND org_id = ANY($12)"; where != want {
		t.Fatalf("where = %q, want %q", where, want)
	}
	if len(args) != 3 || args[0] != "t1" || args[1] != "cust-1" {
		t.Fatalf("args = %v, want tenant, customer, org", args)
	}
	if got := maxPlaceholder(t, where); got != len(args)+9 {
		t.Fatalf("max placeholder $%d does not match 9 reserved + %d args", got, len(args))
	}
}

func TestProjectUpdateStatementKeepsScopeAfterSetPlaceholders(t *testing.T) {
	// Derive the SET parameter count from the rendered statement so adding a
	// SET column without moving the scope predicate fails this test.
	parts := strings.SplitN(projectUpdateStatement(""), "WHERE", 2)
	if len(parts) != 2 {
		t.Fatalf("update statement has no WHERE clause: %q", parts[0])
	}
	setPlaceholders := maxPlaceholder(t, parts[0])
	if setPlaceholders != 7 {
		t.Fatalf("set placeholders = %d, want 7", setPlaceholders)
	}
	for _, orgCodes := range [][]string{nil, {"ORG1"}} {
		where, scopeArgs := projectScopeWhere("t1", "prj-1", orgCodes, setPlaceholders)
		query := projectUpdateStatement(where)
		args := append(make([]any, 0, setPlaceholders+len(scopeArgs)), make([]any, setPlaceholders)...)
		args = append(args, scopeArgs...)
		if got := maxPlaceholder(t, query); got != len(args) {
			t.Fatalf("org=%v: max placeholder $%d, want $%d (query=%q)", orgCodes, got, len(args), query)
		}
		if !strings.Contains(query, where) {
			t.Fatalf("org=%v: scope predicate missing from query %q", orgCodes, query)
		}
	}
}

func TestErrNotFoundMapsToSQLNoRows(t *testing.T) {
	if !errors.Is(ErrNotFound, sql.ErrNoRows) {
		t.Fatal("ErrNotFound must wrap sql.ErrNoRows so WriteServiceError maps it to 404")
	}
	if !errors.Is(scopedNotFound("project", "prj-1"), ErrNotFound) {
		t.Fatal("scopedNotFound must wrap ErrNotFound")
	}
}
