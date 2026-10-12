package ardaopenapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type Address struct {
	Line string `json:"line"`
}

type Base struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type Account struct {
	Base
	Code     string            `json:"code"`
	Balance  int64             `json:"balance_minor"`
	Rate     float64           `json:"rate"`
	Active   bool              `json:"active"`
	Note     *string           `json:"note"`
	Closed   *time.Time        `json:"closed_at,omitempty"`
	Tags     []string          `json:"tags"`
	Labels   map[string]string `json:"labels,omitempty"`
	Address  Address           `json:"address"`
	Previous *Address          `json:"previous,omitempty"`
	Payload  json.RawMessage   `json:"payload,omitempty"`
	Secret   string            `json:"-"`
	internal string
}

type createAccount struct {
	Code string `json:"code"`
}

type Money struct{ minor int64 }

func (m Money) MarshalJSON() ([]byte, error) { return json.Marshal(m.minor) }

type Priced struct {
	Price Money `json:"price"`
}

func testDoc() Document {
	return Document{
		Title: "Test", Version: "1.0.0",
		Operations: []Operation{
			{Method: "GET", Path: "/api/test/accounts", ID: "listAccounts", Result: Account{}, List: true,
				Query: []Param{{Name: "status", Enum: []string{"OPEN", "CLOSED"}}, {Name: "active", Type: "boolean"}}},
			{Method: "POST", Path: "/api/test/accounts", ID: "createAccount", Request: createAccount{}, RequestName: "CreateAccountRequest", Result: &Account{}, Status: 201},
			{Method: "GET", Path: "/api/test/accounts/{code}", ID: "getAccount", Result: Account{}},
		},
	}
}

func generate(t *testing.T, doc Document) map[string]any {
	t.Helper()
	raw, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func schemas(out map[string]any) map[string]any {
	return out["components"].(map[string]any)["schemas"].(map[string]any)
}

func TestGenerateDerivesSchemasFromJSONTags(t *testing.T) {
	account := schemas(generate(t, testDoc()))["Account"].(map[string]any)
	props := account["properties"].(map[string]any)
	for _, name := range []string{"id", "created_at", "code", "balance_minor", "rate", "active", "note", "closed_at", "tags", "labels", "address", "previous", "payload"} {
		if _, ok := props[name]; !ok {
			t.Errorf("property %q missing", name)
		}
	}
	for _, name := range []string{"Secret", "internal", "Base"} {
		if _, ok := props[name]; ok {
			t.Errorf("property %q must not be exported", name)
		}
	}
	required := map[string]bool{}
	for _, r := range account["required"].([]any) {
		required[r.(string)] = true
	}
	for _, name := range []string{"id", "created_at", "code", "balance_minor", "note", "tags", "address"} {
		if !required[name] {
			t.Errorf("%q must be required (no omitempty)", name)
		}
	}
	for _, name := range []string{"closed_at", "labels", "previous", "payload"} {
		if required[name] {
			t.Errorf("%q must be optional (omitempty)", name)
		}
	}
	check := func(name, want string) {
		t.Helper()
		got, _ := json.Marshal(props[name])
		if string(got) != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	check("balance_minor", `{"format":"int64","type":"integer"}`)
	check("created_at", `{"format":"date-time","type":"string"}`)
	check("note", `{"type":["string","null"]}`)
	check("tags", `{"items":{"type":"string"},"type":["array","null"]}`)
	check("address", `{"$ref":"#/components/schemas/Address"}`)
	check("previous", `{"anyOf":[{"$ref":"#/components/schemas/Address"},{"type":"null"}]}`)
	check("payload", `{}`)
	if account["x-go-type"] != "github.com/arda-labs/arda/libs/go/arda-openapi.Account" {
		t.Errorf("x-go-type = %v", account["x-go-type"])
	}
}

func TestGenerateBuildsEnvelopesListsAndParameters(t *testing.T) {
	out := generate(t, testDoc())
	s := schemas(out)
	for _, name := range []string{"ResponseMeta", "Problem", "ProblemError", "AccountSuccess", "AccountListSuccess", "AccountList", "CreateAccountRequest"} {
		if _, ok := s[name]; !ok {
			t.Errorf("schema %s missing", name)
		}
	}
	list := s["AccountList"].(map[string]any)["properties"].(map[string]any)["items"].(map[string]any)
	if list["items"].(map[string]any)["$ref"] != "#/components/schemas/Account" {
		t.Errorf("list items = %v", list)
	}
	paths := out["paths"].(map[string]any)
	create := paths["/api/test/accounts"].(map[string]any)["post"].(map[string]any)
	if _, ok := create["responses"].(map[string]any)["201"]; !ok {
		t.Errorf("create responses = %v", create["responses"])
	}
	get := paths["/api/test/accounts/{code}"].(map[string]any)["get"].(map[string]any)
	param := get["parameters"].([]any)[0].(map[string]any)
	if param["name"] != "code" || param["in"] != "path" || param["required"] != true {
		t.Errorf("path parameter = %v", param)
	}
	if _, ok := get["responses"].(map[string]any)["404"]; !ok {
		t.Error("path with a parameter must document 404")
	}
	listOp := paths["/api/test/accounts"].(map[string]any)["get"].(map[string]any)
	enum, _ := json.Marshal(listOp["parameters"].([]any)[0].(map[string]any)["schema"])
	if string(enum) != `{"enum":["OPEN","CLOSED"],"type":"string"}` {
		t.Errorf("enum parameter = %s", enum)
	}
	scheme := out["components"].(map[string]any)["securitySchemes"].(map[string]any)["ardaSession"].(map[string]any)
	if scheme["x-browser-credentials"] != "include" {
		t.Error("browser credentials must be include")
	}
}

func TestRequestBodiesGetOptionalInputSchemas(t *testing.T) {
	doc := Document{Title: "T", Version: "1", Operations: []Operation{
		{Method: "PUT", Path: "/api/test/accounts", ID: "putAccount", Request: Account{}, Result: Account{}},
		{Method: "POST", Path: "/api/test/accounts", ID: "createAccount", Request: createAccount{}, RequestName: "CreateAccountRequest", Result: Account{}},
	}}
	s := schemas(generate(t, doc))
	input, ok := s["AccountInput"].(map[string]any)
	if !ok {
		t.Fatalf("AccountInput missing: %v", s)
	}
	if _, has := input["required"]; has {
		t.Fatal("request schemas must not require fields that encoding/json treats as optional")
	}
	if _, has := s["Account"].(map[string]any)["required"]; !has {
		t.Fatal("the response schema keeps its required fields")
	}
	if _, has := s["CreateAccountRequest"]; !has {
		t.Fatal("explicit request name not used")
	}
}

func TestPathEnumsRestrictPathParameters(t *testing.T) {
	doc := Document{Title: "T", Version: "1", Operations: []Operation{
		{Method: "GET", Path: "/api/test/{kind}/{id}", ID: "get", Result: Address{}, PathEnums: map[string][]string{"kind": {"a", "b"}}},
	}}
	out := generate(t, doc)
	params := out["paths"].(map[string]any)["/api/test/{kind}/{id}"].(map[string]any)["get"].(map[string]any)["parameters"].([]any)
	kind, _ := json.Marshal(params[0].(map[string]any)["schema"])
	id, _ := json.Marshal(params[1].(map[string]any)["schema"])
	if string(kind) != `{"enum":["a","b"],"type":"string"}` || string(id) != `{"type":"string"}` {
		t.Fatalf("kind=%s id=%s", kind, id)
	}
	bad := Document{Title: "T", Version: "1", Operations: []Operation{
		{Method: "GET", Path: "/api/test/{id}", ID: "get", Result: Address{}, PathEnums: map[string][]string{"kind": {"a"}}},
	}}
	if _, err := Generate(bad); err == nil {
		t.Fatal("an enum for a parameter missing from the path was accepted")
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	first, err := Generate(testDoc())
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		next, err := Generate(testDoc())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatal("output changed between runs")
		}
	}
	if !bytes.HasSuffix(first, []byte("}\n")) {
		t.Fatal("output must end with a newline")
	}
}

func TestGenerateRejectsAmbiguousOrUnsupportedInput(t *testing.T) {
	cases := map[string]Document{
		"missing title":    {Version: "1"},
		"bad method":       {Title: "T", Version: "1", Operations: []Operation{{Method: "HEAD", Path: "/a", ID: "a", Result: Address{}}}},
		"duplicate id":     {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a", Result: Address{}}, {Method: "GET", Path: "/b", ID: "a", Result: Address{}}}},
		"duplicate route":  {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a", Result: Address{}}, {Method: "GET", Path: "/a", ID: "b", Result: Address{}}}},
		"anonymous result": {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a", Result: struct{ X int }{}}}},
		"map result":       {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a", Result: map[string]any{}}}},
		"missing result":   {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a"}}},
		"custom marshaler": {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a", Result: Priced{}}}},
		"non-2xx status":   {Title: "T", Version: "1", Operations: []Operation{{Method: "GET", Path: "/a", ID: "a", Result: Address{}, Status: 302}}},
		"lowercase name":   {Title: "T", Version: "1", Operations: []Operation{{Method: "POST", Path: "/a", ID: "a", Request: createAccount{}, RequestName: "lower", Result: Address{}}}},
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Generate(doc); err == nil {
				t.Fatal("Generate succeeded")
			}
		})
	}
}

func TestGenerateAcceptsTypeSchemaOverride(t *testing.T) {
	doc := Document{
		Title: "T", Version: "1",
		Operations:  []Operation{{Method: "GET", Path: "/a", ID: "a", Result: Priced{}}},
		TypeSchemas: map[string]map[string]any{"github.com/arda-labs/arda/libs/go/arda-openapi.Money": {"type": "integer", "format": "int64"}},
	}
	raw, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"price": {`) {
		t.Fatalf("override not applied:\n%s", raw)
	}
}
