package testdb

import (
	"regexp"
	"testing"
)

func TestNewDatabaseNameIsSafeAndUnique(t *testing.T) {
	namePattern := regexp.MustCompile(`^arda_test_[0-9a-f]{16}$`)
	first := newDatabaseName(t)
	second := newDatabaseName(t)
	if !namePattern.MatchString(first) {
		t.Fatalf("generated database name has invalid format: %q", first)
	}
	if first == second {
		t.Fatal("generated database names must be unique")
	}
}

func TestConnectionConfigUsesTemporaryDatabase(t *testing.T) {
	config, err := connectionConfig("postgres://test-user@127.0.0.1:5432/bootstrap?sslmode=disable&application_name=arda-test&search_path=shared", "arda_test_0123456789abcdef")
	if err != nil {
		t.Fatalf("connectionConfig: %v", err)
	}
	if config.Database != "arda_test_0123456789abcdef" {
		t.Fatalf("database = %q, want the isolated test database", config.Database)
	}
	if config.Host != "127.0.0.1" || config.RuntimeParams["application_name"] != "arda-test" {
		t.Fatalf("connection settings were not preserved: host=%q app=%q", config.Host, config.RuntimeParams["application_name"])
	}
	if config.RuntimeParams["search_path"] != "public" {
		t.Fatalf("search_path = %q, want isolated database public schema", config.RuntimeParams["search_path"])
	}
}
