// catalog-gen reads the internal AI surface contracts
// (contracts/ai-internal/*.json) and generates the committed catalog file
// apps/ai-service/internal/catalog/generated.go.
//
// The catalog is a compile-time artifact: the generated file is committed,
// auditable via git diff, and never parsed from OpenAPI at ai-service
// runtime (see docs/ai/sdk-catalog-design.md §3.3).
//
// Annotation shape (per operation):
//
//	"x-ai-tool": {
//	  "sdkPath": "arda.<domain>.<method>",
//	  "domain": "<domain>",            // must match sdkPath segment
//	  "kind": "read"|"confirm",        // default: GET→read, else confirm
//	  "risk": "low"|"medium"|"high",   // default: medium
//	  "timeoutMs": 3000,               // default: 3000
//	  "keywords": ["..."],             // BM25 search terms
//	  "requiredPerms": ["..."],        // enforced at dispatch, audited in CI
//	  "service": "iam-service",        // target service in the ClientSet
//	  "envelope": "result",            // response envelope key (default "result" when the 200 schema wraps one)
//	  "returns": "TypeName { shape }"  // JSDoc @returns text; first token = TS return type
//	}
//
// Parameters bind SDK arguments to the wire: `x-ai-arg` names the SDK
// argument (default: parameter name), `x-ai-scope` sources the value from
// the verified actor scope instead of tool arguments ("tenant" today),
// `x-ai-transform` applies a value transform ("upper"). The 200 response
// schema is the response allowlist: the executor prunes everything not
// declared there (recursively through $ref/properties/items).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type aiTool struct {
	SDKPath       string   `json:"sdkPath"`
	Domain        string   `json:"domain"`
	Kind          string   `json:"kind"`
	Risk          string   `json:"risk"`
	TimeoutMs     int      `json:"timeoutMs"`
	Keywords      []string `json:"keywords"`
	RequiredPerms []string `json:"requiredPerms"`
	Service       string   `json:"service"`
	Envelope      string   `json:"envelope"`
	Returns       string   `json:"returns"`
	Note          string   `json:"note"`
}

type paramSchema struct {
	Type      string   `json:"type"`
	MaxLength int      `json:"maxLength"`
	Minimum   *float64 `json:"minimum"`
	Maximum   *float64 `json:"maximum"`
	Default   any      `json:"default"`
	Enum      []string `json:"enum"`
}

type parameter struct {
	Name         string      `json:"name"`
	In           string      `json:"in"`
	Required     bool        `json:"required"`
	Description  string      `json:"description"`
	XAIArg       string      `json:"x-ai-arg"`
	XAIScope     string      `json:"x-ai-scope"`
	XAITransform string      `json:"x-ai-transform"`
	Schema       paramSchema `json:"schema"`
}

type operation struct {
	OperationID string          `json:"operationId"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	XAITool     *aiTool         `json:"x-ai-tool"`
	Parameters  []parameter     `json:"parameters"`
	RequestBody *requestBody    `json:"requestBody"`
	Responses   json.RawMessage `json:"responses"`
}

type requestBody struct {
	Content map[string]struct {
		Schema rawSchema `json:"schema"`
	} `json:"content"`
}

// rawSchema is the permissive decode shape used while walking the response
// schema (properties/items/$ref) before normalization.
type rawSchema struct {
	Type       string               `json:"type"`
	Ref        string               `json:"$ref"`
	Properties map[string]rawSchema `json:"properties"`
	Items      *rawSchema           `json:"items"`
	Enum       []string             `json:"enum"`
	Default    any                  `json:"default"`
}

type document struct {
	Info struct {
		Title string `json:"title"`
	} `json:"info"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]rawSchema `json:"schemas"`
	} `json:"components"`
}

// pruneNode is the normalized response allowlist: everything not expressible
// as object-properties/array-items/type is dropped.
type pruneNode struct {
	Type       string                `json:"type,omitempty"`
	Properties map[string]*pruneNode `json:"properties,omitempty"`
	Items      *pruneNode            `json:"items,omitempty"`
}

func normalize(raw rawSchema, doc *document, seen map[string]bool) *pruneNode {
	if raw.Ref != "" {
		name := strings.TrimPrefix(raw.Ref, "#/components/schemas/")
		if seen[name] {
			return nil
		}
		sub, ok := doc.Components.Schemas[name]
		if !ok {
			return nil
		}
		seen[name] = true
		defer delete(seen, name)
		return normalize(sub, doc, seen)
	}
	node := &pruneNode{Type: raw.Type}
	if len(raw.Properties) > 0 {
		node.Properties = make(map[string]*pruneNode, len(raw.Properties))
		names := make([]string, 0, len(raw.Properties))
		for name := range raw.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			node.Properties[name] = normalize(raw.Properties[name], doc, seen)
		}
	}
	if raw.Items != nil {
		node.Items = normalize(*raw.Items, doc, seen)
	}
	return node
}

func mustJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "{}"
	}
	return strings.TrimRight(buf.String(), "\n")
}

type goArg struct {
	Name        string
	Param       string
	In          string
	Required    bool
	Type        string
	MaxLength   int
	Min         *float64
	Max         *float64
	Default     string
	Transform   string
	Enum        []string
	Description string
}

type goEntry struct {
	SDKPath             string
	Domain              string
	Signature           string
	JSDoc               string
	Keywords            []string
	Kind                string
	RequiredPermissions []string
	Risk                string
	TimeoutMs           int
	Service             string
	Method              string
	Path                string
	Envelope            string
	Args                []goArg
	ScopeQuery          []scopeQueryArg
	ResponseSchema      string
	Note                string
}

// scopeQueryArg binds a query parameter to a verified scope source ("tenant").
type scopeQueryArg struct {
	Param string
	Scope string
}

func main() {
	contractsDir := flag.String("contracts", "contracts/ai-internal", "directory of internal AI surface OpenAPI documents")
	outFile := flag.String("out", "apps/ai-service/internal/catalog/generated.go", "generated Go output file")
	check := flag.Bool("check", false, "verify the output file is up to date without writing; exit 1 on drift")
	flag.Parse()

	entries, err := loadContracts(*contractsDir)
	if err != nil {
		fatal("%v", err)
	}
	if len(entries) == 0 {
		fatal("no x-ai-tool annotations found in %s", *contractsDir)
	}

	code := render(entries)
	formatted, err := format.Source([]byte(code))
	if err != nil {
		// Dump the unformatted source to ease fixing the emitter.
		fmt.Fprintln(os.Stderr, code)
		fatal("gofmt: %v", err)
	}
	existing, readErr := os.ReadFile(*outFile)
	if *check {
		if readErr != nil {
			fatal("--check: cannot read %s: %v", *outFile, readErr)
		}
		if string(existing) != string(formatted) {
			fatal("--check: %s is stale — run `go run ./tools/catalog-gen` and commit the result", *outFile)
		}
		fmt.Printf("catalog-gen: %s up to date (%d entries)\n", *outFile, len(entries))
		return
	}
	if err := os.MkdirAll(filepath.Dir(*outFile), 0o755); err != nil {
		fatal("mkdir: %v", err)
	}
	if readErr == nil && string(existing) == string(formatted) {
		fmt.Printf("catalog-gen: %s up to date (%d entries)\n", *outFile, len(entries))
		return
	}
	if err := os.WriteFile(*outFile, formatted, 0o644); err != nil {
		fatal("write: %v", err)
	}
	fmt.Printf("catalog-gen: wrote %s (%d entries)\n", *outFile, len(entries))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "catalog-gen: "+format+"\n", args...)
	os.Exit(1)
}

func loadContracts(dir string) ([]goEntry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no OpenAPI documents found in %s", dir)
	}
	sort.Strings(files)

	var entries []goEntry
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var doc document
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if doc.Info.Title == "" {
			return nil, fmt.Errorf("%s: missing info.title", file)
		}
		docEntries, err := docEntries(file, &doc)
		if err != nil {
			return nil, err
		}
		entries = append(entries, docEntries...)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].SDKPath < entries[j].SDKPath })
	for i := 1; i < len(entries); i++ {
		if entries[i].SDKPath == entries[i-1].SDKPath {
			return nil, fmt.Errorf("duplicate sdkPath %s", entries[i].SDKPath)
		}
	}
	return entries, nil
}

func docEntries(file string, doc *document) ([]goEntry, error) {
	var entries []goEntry
	routes := make([]string, 0, len(doc.Paths))
	for route := range doc.Paths {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		methods := make([]string, 0, len(doc.Paths[route]))
		for method := range doc.Paths[route] {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			op := doc.Paths[route][method]
			if op.XAITool == nil {
				continue
			}
			entry, err := buildEntry(file, doc, route, method, op)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

var httpMethods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

func buildEntry(file string, doc *document, route, method string, op operation) (goEntry, error) {
	tool := op.XAITool
	if tool.SDKPath == "" || tool.Domain == "" || tool.Service == "" {
		return goEntry{}, fmt.Errorf("%s: %s %s: x-ai-tool requires sdkPath, domain, service", file, method, route)
	}
	if !httpMethods[method] {
		return goEntry{}, fmt.Errorf("%s: %s %s: unsupported method", file, method, route)
	}
	kind := tool.Kind
	if kind == "" {
		// WP5 default: GET is a read, anything else is a mutation that must
		// pass human approval before producing side effects.
		if method == "get" {
			kind = "read"
		} else {
			kind = "confirm"
		}
	}
	risk := tool.Risk
	if risk == "" {
		risk = "medium"
	}
	timeoutMs := tool.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}

	summary := strings.TrimSpace(op.Summary)
	desc := strings.TrimSpace(op.Description)
	if desc == "" {
		desc = summary
	}

	var args []goArg
	var scopeQuery []scopeQueryArg
	for _, p := range op.Parameters {
		if p.XAIScope != "" {
			scopeQuery = append(scopeQuery, scopeQueryArg{Param: p.Name, Scope: p.XAIScope})
			continue
		}
		arg := p.XAIArg
		if arg == "" {
			arg = p.Name
		}
		a := goArg{
			Name:        arg,
			Param:       p.Name,
			In:          p.In,
			Required:    p.Required,
			Type:        p.Schema.Type,
			MaxLength:   p.Schema.MaxLength,
			Min:         p.Schema.Minimum,
			Max:         p.Schema.Maximum,
			Transform:   p.XAITransform,
			Enum:        p.Schema.Enum,
			Description: p.Description,
		}
		if p.Schema.Default != nil {
			a.Default = fmt.Sprintf("%v", p.Schema.Default)
		}
		args = append(args, a)
	}
	// requestBody properties become body arguments (in: "body").
	if op.RequestBody != nil {
		if body, ok := op.RequestBody.Content["application/json"]; ok {
			names := make([]string, 0, len(body.Schema.Properties))
			for name := range body.Schema.Properties {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				s := body.Schema.Properties[name]
				a := goArg{In: "body", Param: name, Name: name, Type: s.Type, Enum: s.Enum}
				if s.Default != nil {
					a.Default = fmt.Sprintf("%v", s.Default)
				}
				args = append(args, a)
			}
		}
	}

	entry := goEntry{
		SDKPath:             tool.SDKPath,
		Domain:              tool.Domain,
		Kind:                kind,
		Keywords:            tool.Keywords,
		RequiredPermissions: tool.RequiredPerms,
		Risk:                risk,
		TimeoutMs:           timeoutMs,
		Service:             tool.Service,
		Method:              strings.ToUpper(method),
		Path:                route,
		Args:                args,
		ScopeQuery:          scopeQuery,
		Note:                tool.Note,
	}
	entry.Signature = buildSignature(tool.SDKPath, args, tool.Returns)
	entry.JSDoc = buildJSDoc(summary, desc, args, tool)
	if strings.ContainsAny(entry.JSDoc+entry.Signature, "`") {
		return goEntry{}, fmt.Errorf("%s: %s: annotation text must not contain backticks", file, tool.SDKPath)
	}

	// Response allowlist from the 200 response schema. When the schema wraps
	// the payload in an envelope, unwrap it — the executor prunes the payload
	// after decoding the envelope — and record the envelope key.
	resultSchema, envelope, err := responseSchema(&op, doc)
	if err != nil {
		return goEntry{}, fmt.Errorf("%s: %s: %v", file, tool.SDKPath, err)
	}
	entry.Envelope = tool.Envelope
	if entry.Envelope == "" {
		entry.Envelope = envelope
	}
	if resultSchema != nil {
		entry.ResponseSchema = mustJSON(resultSchema)
	}
	return entry, nil
}

// responseSchema locates the 200 response schema, resolves the envelope
// wrapper (a top-level "result" property) and normalizes it into a pruneNode.
func responseSchema(op *operation, doc *document) (*pruneNode, string, error) {
	var responses struct {
		Content map[string]struct {
			Schema rawSchema `json:"schema"`
		} `json:"content"`
	}
	var found bool
	var raw json.RawMessage
	// Responses were decoded as RawMessage; re-decode to find the 2xx entry.
	var respMap map[string]json.RawMessage
	if err := json.Unmarshal(op.Responses, &respMap); err != nil {
		return nil, "", err
	}
	for _, status := range []string{"200", "201", "202"} {
		if r, ok := respMap[status]; ok {
			raw = r
			found = true
			break
		}
	}
	if !found {
		for status, r := range respMap {
			if strings.HasPrefix(status, "2") {
				raw = r
				found = true
				break
			}
		}
	}
	if !found {
		return nil, "", fmt.Errorf("no 2xx response declared")
	}
	if err := json.Unmarshal(raw, &responses); err != nil {
		return nil, "", err
	}
	var schema rawSchema
	for _, media := range responses.Content {
		schema = media.Schema
		break
	}
	if schema.Ref != "" {
		schema = deref(schema, doc)
	}
	// Unwrap envelope: schema.properties.result.
	if result, ok := schema.Properties["result"]; ok {
		node := normalize(result, doc, map[string]bool{})
		return node, "result", nil
	}
	if schema.Properties != nil {
		node := normalize(schema, doc, map[string]bool{})
		return node, "", nil
	}
	return nil, "", fmt.Errorf("200 response schema has no prunable properties")
}

func deref(s rawSchema, doc *document) rawSchema {
	name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
	sub, ok := doc.Components.Schemas[name]
	if !ok {
		return s
	}
	return sub
}

func buildSignature(sdkPath string, args []goArg, returns string) string {
	var b strings.Builder
	b.WriteString(sdkPath + "(args: {")
	for i, arg := range args {
		if i > 0 {
			b.WriteString("; ")
		}
		if !arg.Required {
			b.WriteString(arg.Name + "?: ")
		} else {
			b.WriteString(arg.Name + ": ")
		}
		switch arg.Type {
		case "integer", "number":
			b.WriteString("number")
		case "boolean":
			b.WriteString("boolean")
		default:
			b.WriteString("string")
		}
	}
	b.WriteString("}): Promise<")
	b.WriteString(returnTypeName(returns))
	b.WriteString(">;")
	return b.String()
}

func returnTypeName(returns string) string {
	if returns == "" {
		return "unknown"
	}
	fields := strings.Fields(returns)
	return fields[0]
}

func buildJSDoc(summary, desc string, args []goArg, tool *aiTool) string {
	var b strings.Builder
	b.WriteString("/**\n * " + summary + "\n")
	if desc != "" && desc != summary {
		b.WriteString(" *\n * " + strings.ReplaceAll(desc, "\n", " ") + "\n")
	}
	for _, arg := range args {
		b.WriteString(" * @param args." + arg.Name)
		if arg.Description != "" {
			b.WriteString(" " + strings.ReplaceAll(arg.Description, "\n", " "))
		}
		b.WriteString("\n")
	}
	if tool.Returns != "" {
		b.WriteString(" * @returns " + tool.Returns + "\n")
	}
	if len(tool.RequiredPerms) > 0 {
		b.WriteString(" * @requires " + strings.Join(tool.RequiredPerms, ", ") + "\n")
	}
	if tool.Note != "" {
		b.WriteString(" * @note " + tool.Note + "\n")
	}
	b.WriteString(" * @domain " + tool.Domain + "\n */")
	return b.String()
}

func render(entries []goEntry) string {
	var b strings.Builder
	b.WriteString(`// Code generated by tools/catalog-gen from contracts/ai-internal/*.json. DO NOT EDIT.
// Regenerate: go run ./tools/catalog-gen (from the arda-be root).

package catalog

import "time"

// GeneratedEntry is one tool sourced from an internal AI surface contract.
type GeneratedEntry struct {
	SDKPath             string
	Domain              string
	Signature           string
	JSDoc               string
	Keywords            []string
	Kind                string
	RequiredPermissions []string
	Risk                string
	Timeout             time.Duration
	Service             string
	Method              string
	Path                string
	Envelope            string
	Args                []GeneratedArg
	ScopeQuery          []GeneratedScopeQuery
	ResponseSchema      string
	Note                string
}

// GeneratedScopeQuery binds a query parameter to a verified scope source
// ("tenant" today) — the value never comes from tool arguments.
type GeneratedScopeQuery struct {
	Param string
	Scope string
}

// GeneratedArg binds an SDK argument to an HTTP parameter binding.
type GeneratedArg struct {
	Name      string   // SDK argument name (camelCase)
	Param     string   // HTTP parameter name on the wire
	In        string   // "query" | "path" | "body"
	Required  bool
	Type      string   // "string" | "integer" | "boolean"
	MaxLength int
	Min       *float64
	Max       *float64
	Default   string
	Transform string
	Enum      []string
}

// GeneratedCatalog is the catalog derived from contracts/ai-internal/*.json.
// Domain registrars keep hand-written entries for self-service methods and
// orchestration; GeneratedCatalog covers direct internal HTTP reads/mutations.
func GeneratedCatalog() []GeneratedEntry {
	return []GeneratedEntry{
`)
	for _, e := range entries {
		fmt.Fprintf(&b, "\t\t{\n")
		fmt.Fprintf(&b, "\t\t\tSDKPath: %q,\n", e.SDKPath)
		fmt.Fprintf(&b, "\t\t\tDomain: %q,\n", e.Domain)
		fmt.Fprintf(&b, "\t\t\tSignature: %q,\n", e.Signature)
		fmt.Fprintf(&b, "\t\t\tJSDoc: `%s`,\n", e.JSDoc)
		fmt.Fprintf(&b, "\t\t\tKeywords: []string{%s},\n", quotedList(e.Keywords))
		fmt.Fprintf(&b, "\t\t\tKind: %q,\n", e.Kind)
		fmt.Fprintf(&b, "\t\t\tRequiredPermissions: []string{%s},\n", quotedList(e.RequiredPermissions))
		fmt.Fprintf(&b, "\t\t\tRisk: %q,\n", e.Risk)
		fmt.Fprintf(&b, "\t\t\tTimeout: %d * time.Millisecond,\n", e.TimeoutMs)
		fmt.Fprintf(&b, "\t\t\tService: %q,\n", e.Service)
		fmt.Fprintf(&b, "\t\t\tMethod: %q,\n", e.Method)
		fmt.Fprintf(&b, "\t\t\tPath: %q,\n", e.Path)
		fmt.Fprintf(&b, "\t\t\tEnvelope: %q,\n", e.Envelope)
		fmt.Fprintf(&b, "\t\t\tArgs: []GeneratedArg{\n")
		for _, a := range e.Args {
			fmt.Fprintf(&b, "\t\t\t\t{Name: %q, Param: %q, In: %q, Required: %v, Type: %q, MaxLength: %d, Min: %s, Max: %s, Default: %q, Transform: %q, Enum: []string{%s}},\n",
				a.Name, a.Param, a.In, a.Required, a.Type, a.MaxLength, floatPtr(a.Min), floatPtr(a.Max), a.Default, a.Transform, quotedList(a.Enum))
		}
		fmt.Fprintf(&b, "\t\t\t},\n")
		fmt.Fprintf(&b, "\t\t\tScopeQuery: []GeneratedScopeQuery{\n")
		for _, sq := range e.ScopeQuery {
			fmt.Fprintf(&b, "\t\t\t\t{Param: %q, Scope: %q},\n", sq.Param, sq.Scope)
		}
		fmt.Fprintf(&b, "\t\t\t},\n")
		fmt.Fprintf(&b, "\t\t\tResponseSchema: `%s`,\n", e.ResponseSchema)
		if e.Note != "" {
			fmt.Fprintf(&b, "\t\t\tNote: %q,\n", e.Note)
		}
		fmt.Fprintf(&b, "\t\t},\n")
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func quotedList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = fmt.Sprintf("%q", item)
	}
	return strings.Join(parts, ", ")
}

func floatPtr(v *float64) string {
	if v == nil {
		return "nil"
	}
	// Emit a float literal so the generated *float64 conversion type-checks.
	return fmt.Sprintf("ptr(%s)", strconv.FormatFloat(*v, 'f', 1, 64))
}
