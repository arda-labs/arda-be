package http

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	ardaopenapi "github.com/arda-labs/arda/libs/go/arda-openapi"
)

// committedDocument is the contract the frontend and docs-site consume.
var committedDocument = filepath.Join("..", "..", "..", "..", "..", "contracts", "openapi", "loan-v1.json")

// TestOpenAPIDocumentIsCommitted fails when contracts/openapi/loan-v1.json
// differs from the document generated from OpenAPIDocument. Regenerate with:
//
//	ARDA_UPDATE_OPENAPI=1 go test ./internal/transport/http -run TestOpenAPIDocumentIsCommitted
func TestOpenAPIDocumentIsCommitted(t *testing.T) {
	generated, err := ardaopenapi.Generate(OpenAPIDocument())
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("ARDA_UPDATE_OPENAPI") == "1" {
		if err := os.WriteFile(committedDocument, generated, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", committedDocument)
		return
	}
	committed, err := os.ReadFile(committedDocument)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with ARDA_UPDATE_OPENAPI=1)", committedDocument, err)
	}
	// A Windows checkout may add CRLF to the working copy.
	committed = bytes.ReplaceAll(committed, []byte{'\r', '\n'}, []byte{'\n'})
	if !bytes.Equal(committed, generated) {
		t.Fatalf("%s is stale; regenerate with ARDA_UPDATE_OPENAPI=1 go test ./internal/transport/http -run TestOpenAPIDocumentIsCommitted", committedDocument)
	}
}
