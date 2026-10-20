package repository

// Chặn tĩnh AP-TEN-02 (chạy không cần Postgres): câu SQL ghi (INSERT/UPDATE/
// DELETE) trong repository mdm không được dùng điều kiện "tenant_id IS NULL OR",
// vì điều kiện đó mở hàng toàn cục cho mọi tenant. "OR NULL" chỉ hợp lệ trong
// câu SELECT; hàng toàn cục chỉ đổi qua migration.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	writeStatement = regexp.MustCompile(`(?is)^\s*(INSERT|UPDATE|DELETE)\b`)
	globalOrTenant = regexp.MustCompile(`(?i)tenant_id\s+IS\s+NULL\s+OR`)
)

// writeSQLWithGlobalScope trả về các câu SQL ghi có mẫu "tenant_id IS NULL OR".
func writeSQLWithGlobalScope(t *testing.T, file string, src any) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var bad []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if writeStatement.MatchString(value) && globalOrTenant.MatchString(value) {
			bad = append(bad, strings.TrimSpace(value))
		}
		return true
	})
	return bad
}

func TestWriteSQLNeverMatchesGlobalRows(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range writeSQLWithGlobalScope(t, file, src) {
			t.Errorf("%s: câu ghi khớp cả hàng toàn cục (tenant_id IS NULL OR ...); dùng tenant_id = $tenant:\n%s", file, stmt)
		}
	}
}

func TestWriteSQLGuardFlagsViolation(t *testing.T) {
	src := "package x\n" +
		"const a = `UPDATE t SET v = 1 FROM h WHERE h.id = t.h_id AND (h.tenant_id IS NULL OR h.tenant_id = $2)`\n" +
		"const b = `DELETE FROM t WHERE (tenant_id IS NULL OR tenant_id = $1)`\n" +
		"const c = `SELECT * FROM t WHERE (tenant_id IS NULL OR tenant_id = $1)`\n"
	if got := writeSQLWithGlobalScope(t, "x.go", src); len(got) != 2 {
		t.Fatalf("flagged %d statements, want 2 (UPDATE + DELETE, not SELECT): %q", len(got), got)
	}
}
