// Package ardaopenapi builds an OpenAPI 3.1 document for a service's public
// REST surface from Go declarations: the operations are listed in Go and every
// request and result schema is derived from the real Go type through its json
// tags. The committed document under contracts/openapi stays the contract that
// the frontend and docs-site consume (ADR-005); a service test regenerates it
// and fails when the committed copy differs, so it cannot drift from the code.
//
// Responses follow the canonical arda-http envelope: a success body is
// {result, success, errors, messages, meta}, a list result is
// {items, page, per_page, total}, and errors are application/problem+json.
package ardaopenapi

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Param is a query parameter.
type Param struct {
	Name        string
	Description string
	Required    bool
	// Type is the JSON Schema type: "string" (default), "integer" or "boolean".
	Type string
	// Enum restricts a string parameter.
	Enum []string
}

// Operation is one public REST operation.
type Operation struct {
	Method  string
	Path    string
	ID      string
	Summary string
	Query   []Param
	// Request is a value of the JSON body type, or nil when there is no body.
	Request any
	// RequestName names an anonymous request struct.
	RequestName string
	// Result is a value of the envelope result type. For a list it is the
	// item type.
	Result any
	// ResultName names an anonymous or map result.
	ResultName string
	// List marks a result wrapped in {items, page, per_page, total}.
	List bool
	// Status is the success status code (200 when zero).
	Status int
	// Problems lists the documented problem status codes. Empty means
	// 400, 401, 403 and 500, plus 404 when the path has a parameter.
	Problems []int
}

// Document is the service surface.
type Document struct {
	Title       string
	Version     string
	Description string
	Operations  []Operation
	// TypeSchemas supplies the schema for a Go type the generator cannot
	// derive, such as one with a custom MarshalJSON, keyed by the type's
	// package path and name (for example "github.com/x/money.Amount").
	TypeSchemas map[string]map[string]any
}

const (
	schemaRef  = "#/components/schemas/"
	problemRef = "#/components/responses/Problem"
)

var (
	pathParamPattern = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	namePattern      = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	timeType         = reflect.TypeOf(time.Time{})
	rawMessageType   = reflect.TypeOf(json.RawMessage{})
	marshalerType    = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler    = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

type generator struct {
	doc     Document
	schemas map[string]any
	owners  map[string]reflect.Type
	inputs  map[string]reflect.Type
}

// Generate returns the document as indented JSON with a trailing newline.
// Output is deterministic: object keys are sorted.
func Generate(doc Document) ([]byte, error) {
	g := &generator{doc: doc, schemas: map[string]any{}, owners: map[string]reflect.Type{}, inputs: map[string]reflect.Type{}}
	if strings.TrimSpace(doc.Title) == "" || strings.TrimSpace(doc.Version) == "" {
		return nil, fmt.Errorf("openapi: document title and version are required")
	}
	g.addStandardSchemas()

	paths := map[string]map[string]any{}
	seenIDs := map[string]bool{}
	for _, op := range doc.Operations {
		method := strings.ToLower(strings.TrimSpace(op.Method))
		switch method {
		case "get", "post", "put", "patch", "delete":
		default:
			return nil, fmt.Errorf("openapi: %s %s: unsupported method", op.Method, op.Path)
		}
		if !strings.HasPrefix(op.Path, "/") {
			return nil, fmt.Errorf("openapi: %s %s: path must start with /", op.Method, op.Path)
		}
		if op.ID == "" || seenIDs[op.ID] {
			return nil, fmt.Errorf("openapi: %s %s: operation id %q is empty or duplicated", op.Method, op.Path, op.ID)
		}
		seenIDs[op.ID] = true
		item := paths[op.Path]
		if item == nil {
			item = map[string]any{}
			paths[op.Path] = item
		}
		if _, dup := item[method]; dup {
			return nil, fmt.Errorf("openapi: %s %s declared twice", op.Method, op.Path)
		}
		built, err := g.operation(op)
		if err != nil {
			return nil, err
		}
		item[method] = built
	}

	out := map[string]any{
		"openapi":  "3.1.0",
		"info":     map[string]any{"title": doc.Title, "version": doc.Version},
		"servers":  []any{map[string]any{"url": "https://api.arda.io.vn"}},
		"security": []any{map[string]any{"ardaSession": []any{}}},
		"paths":    paths,
		"components": map[string]any{
			"schemas": g.schemas,
			"responses": map[string]any{
				"Problem": map[string]any{
					"description": "RFC-style problem response",
					"content": map[string]any{
						"application/problem+json": map[string]any{"schema": ref("Problem")},
					},
				},
			},
			"securitySchemes": map[string]any{
				"ardaSession": map[string]any{
					"type": "apiKey", "in": "cookie", "name": "arda_session",
					"x-browser-credentials": "include",
				},
			},
		},
	}
	if doc.Description != "" {
		out["info"].(map[string]any)["description"] = doc.Description
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ref(name string) map[string]any { return map[string]any{"$ref": schemaRef + name} }

func (g *generator) addStandardSchemas() {
	str := map[string]any{"type": "string"}
	g.schemas["ResponseMeta"] = map[string]any{
		"type": "object", "required": []any{"request_id"},
		"properties": map[string]any{
			"request_id": str,
			"trace_id":   str,
			"timestamp":  map[string]any{"type": "string", "format": "date-time"},
		},
	}
	g.schemas["ProblemError"] = map[string]any{
		"type": "object", "required": []any{"message"},
		"properties": map[string]any{"code": str, "message": str, "field": str, "path": str},
	}
	g.schemas["Problem"] = map[string]any{
		"type": "object", "required": []any{"type", "title", "status", "code", "message"},
		"properties": map[string]any{
			"type":       map[string]any{"type": "string", "format": "uri-reference"},
			"title":      str,
			"status":     map[string]any{"type": "integer"},
			"code":       str,
			"message":    str,
			"errors":     map[string]any{"type": "array", "items": ref("ProblemError")},
			"request_id": str,
			"trace_id":   str,
		},
	}
}

func (g *generator) operation(op Operation) (map[string]any, error) {
	out := map[string]any{"operationId": op.ID}
	if op.Summary != "" {
		out["summary"] = op.Summary
	}
	var params []any
	pathParams := pathParamPattern.FindAllStringSubmatch(op.Path, -1)
	for _, m := range pathParams {
		params = append(params, map[string]any{
			"name": m[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"},
		})
	}
	for _, q := range op.Query {
		schema := map[string]any{"type": "string"}
		switch q.Type {
		case "", "string":
		case "integer", "boolean":
			schema["type"] = q.Type
		default:
			return nil, fmt.Errorf("openapi: %s: query %s has unsupported type %q", op.ID, q.Name, q.Type)
		}
		if len(q.Enum) > 0 {
			values := make([]any, len(q.Enum))
			for i, v := range q.Enum {
				values[i] = v
			}
			schema["enum"] = values
		}
		param := map[string]any{"name": q.Name, "in": "query", "required": q.Required, "schema": schema}
		if q.Description != "" {
			param["description"] = q.Description
		}
		params = append(params, param)
	}
	if len(params) > 0 {
		out["parameters"] = params
	}

	if op.Request != nil {
		schema, err := g.inputSchema(reflect.TypeOf(op.Request), op.RequestName, op.ID+" request")
		if err != nil {
			return nil, err
		}
		out["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": schema}},
		}
	}

	status := op.Status
	if status == 0 {
		status = http.StatusOK
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("openapi: %s: success status %d is not 2xx", op.ID, status)
	}
	if op.Result == nil {
		return nil, fmt.Errorf("openapi: %s: result type is required", op.ID)
	}
	resultSchema, resultName, err := g.resultSchema(op)
	if err != nil {
		return nil, err
	}
	envelope := resultName + "Success"
	g.schemas[envelope] = successEnvelope(resultSchema)
	responses := map[string]any{
		fmt.Sprint(status): map[string]any{
			"description": http.StatusText(status),
			"content":     map[string]any{"application/json": map[string]any{"schema": ref(envelope)}},
		},
	}
	problems := op.Problems
	if len(problems) == 0 {
		problems = []int{400, 401, 403, 500}
		if len(pathParams) > 0 {
			problems = append(problems, 404)
		}
	}
	for _, code := range problems {
		responses[fmt.Sprint(code)] = map[string]any{"$ref": problemRef}
	}
	out["responses"] = responses
	return out, nil
}

func successEnvelope(result map[string]any) map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"result", "success", "errors", "messages", "meta"},
		"properties": map[string]any{
			"result":   result,
			"success":  map[string]any{"const": true},
			"errors":   map[string]any{"type": "array", "maxItems": 0},
			"messages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"meta":     ref("ResponseMeta"),
		},
	}
}

// resultSchema returns the schema for the envelope result and the base name
// used for the envelope schema.
func (g *generator) resultSchema(op Operation) (map[string]any, string, error) {
	t := reflect.TypeOf(op.Result)
	item, err := g.namedSchema(t, op.ResultName, op.ID+" result")
	if err != nil {
		return nil, "", err
	}
	name := op.ResultName
	if name == "" {
		name = deref(t).Name()
	}
	if !op.List {
		return item, name, nil
	}
	listName := name + "List"
	if existing, ok := g.schemas[listName]; ok {
		if _, isList := existing.(map[string]any)["x-arda-list"]; !isList {
			return nil, "", fmt.Errorf("openapi: %s: schema name %s is taken", op.ID, listName)
		}
	}
	g.schemas[listName] = map[string]any{
		"type":        "object",
		"x-arda-list": true,
		"required":    []any{"items", "page", "per_page", "total"},
		"properties": map[string]any{
			"items":    map[string]any{"type": "array", "items": item},
			"page":     map[string]any{"type": "integer", "minimum": 1},
			"per_page": map[string]any{"type": "integer", "minimum": 1},
			"total":    map[string]any{"type": "integer", "minimum": 0},
		},
	}
	return ref(listName), listName, nil
}

// namedSchema registers t as a component schema and returns a $ref to it.
// Anonymous structs and maps need an explicit name.
func (g *generator) namedSchema(t reflect.Type, name, where string) (map[string]any, error) {
	base := deref(t)
	if name == "" {
		name = base.Name()
	}
	if name == "" {
		return nil, fmt.Errorf("openapi: %s: anonymous %s type needs an explicit name", where, base.Kind())
	}
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("openapi: %s: schema name %q must be PascalCase", where, name)
	}
	// An explicit name wins over the Go type name; anonymous structs and maps
	// always need one.
	if base.Kind() == reflect.Struct && name != base.Name() || base.Kind() == reflect.Map {
		if owner, ok := g.owners[name]; ok && owner != base {
			return nil, fmt.Errorf("openapi: %s: schema name %s already names another type", where, name)
		}
		if _, ok := g.owners[name]; !ok {
			g.owners[name] = base
			schema, err := g.schemaFor(base, nil)
			if err != nil {
				return nil, fmt.Errorf("openapi: %s: %w", where, err)
			}
			if base.Name() != "" {
				schema["x-go-type"] = typeKey(base)
			}
			g.schemas[name] = schema
		}
		return ref(name), nil
	}
	return g.typeSchema(base, nil)
}

// inputSchema registers a request body schema. encoding/json fills whatever
// fields are present and leaves the rest at their zero value, so every
// property of a request schema is optional; the service validates. A type
// shared with responses therefore gets its own input schema: RequestName when
// given, the Go name when it already ends in Request or Input, otherwise the
// Go name plus "Input".
func (g *generator) inputSchema(t reflect.Type, name, where string) (map[string]any, error) {
	base := deref(t)
	if base.Kind() != reflect.Struct {
		return nil, fmt.Errorf("openapi: %s: request body must be a struct, got %s", where, base.Kind())
	}
	if name == "" {
		name = base.Name()
		if name == "" {
			return nil, fmt.Errorf("openapi: %s: anonymous request struct needs an explicit name", where)
		}
		if !strings.HasSuffix(name, "Request") && !strings.HasSuffix(name, "Input") {
			name += "Input"
		}
	}
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("openapi: %s: schema name %q must be PascalCase", where, name)
	}
	if owner, ok := g.inputs[name]; ok {
		if owner != base {
			return nil, fmt.Errorf("openapi: %s: input schema name %s already names another type", where, name)
		}
		return ref(name), nil
	}
	if _, taken := g.owners[name]; taken {
		return nil, fmt.Errorf("openapi: %s: input schema name %s collides with a response schema", where, name)
	}
	g.inputs[name] = base
	schema, err := g.schemaFor(base, nil)
	if err != nil {
		return nil, fmt.Errorf("openapi: %s: %w", where, err)
	}
	delete(schema, "required")
	if base.Name() != "" {
		schema["x-go-type"] = typeKey(base)
	}
	g.schemas[name] = schema
	return ref(name), nil
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func typeKey(t reflect.Type) string { return t.PkgPath() + "." + t.Name() }

// typeSchema returns the schema for a field or element type. Named structs
// become component schemas referenced by name.
func (g *generator) typeSchema(t reflect.Type, stack []reflect.Type) (map[string]any, error) {
	nullable := false
	for t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}
	schema, err := g.valueSchema(t, stack)
	if err != nil {
		return nil, err
	}
	if nullable {
		return nullableSchema(schema), nil
	}
	return schema, nil
}

func nullableSchema(schema map[string]any) map[string]any {
	if typ, ok := schema["type"].(string); ok && len(schema) <= 2 {
		copyOf := map[string]any{}
		for k, v := range schema {
			copyOf[k] = v
		}
		copyOf["type"] = []any{typ, "null"}
		return copyOf
	}
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

func (g *generator) valueSchema(t reflect.Type, stack []reflect.Type) (map[string]any, error) {
	if t.Name() != "" && t.PkgPath() != "" {
		if custom, ok := g.doc.TypeSchemas[typeKey(t)]; ok {
			return custom, nil
		}
	}
	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}, nil
	case t == rawMessageType:
		return map[string]any{}, nil
	case t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType):
		return nil, fmt.Errorf("type %s has a custom MarshalJSON; add it to Document.TypeSchemas", typeKey(t))
	case t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler):
		return nil, fmt.Errorf("type %s has a custom MarshalText; add it to Document.TypeSchemas", typeKey(t))
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer"}, nil
	case reflect.Int64, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Interface:
		return map[string]any{}, nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}, nil
		}
		items, err := g.typeSchema(t.Elem(), stack)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key %s is not a string", t.Key())
		}
		values, err := g.typeSchema(t.Elem(), stack)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": values}, nil
	case reflect.Struct:
		if t.Name() == "" {
			return g.schemaFor(t, stack)
		}
		name := t.Name()
		if owner, ok := g.owners[name]; ok {
			if owner != t {
				return nil, fmt.Errorf("two Go types are named %s (%s and %s); give one an explicit name", name, typeKey(owner), typeKey(t))
			}
			return ref(name), nil
		}
		g.owners[name] = t
		schema, err := g.schemaFor(t, append(stack, t))
		if err != nil {
			return nil, err
		}
		schema["x-go-type"] = typeKey(t)
		g.schemas[name] = schema
		return ref(name), nil
	default:
		return nil, fmt.Errorf("unsupported kind %s", t.Kind())
	}
}

// schemaFor builds an object schema following encoding/json field rules:
// exported fields only, json tag names, "-" skipped, omitempty fields
// optional, embedded structs without a tag name flattened.
func (g *generator) schemaFor(t reflect.Type, stack []reflect.Type) (map[string]any, error) {
	if t.Kind() == reflect.Map {
		return g.valueSchema(t, stack)
	}
	props := map[string]any{}
	var required []string
	if err := g.collectFields(t, stack, props, &required); err != nil {
		return nil, err
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		sort.Strings(required)
		list := make([]any, len(required))
		for i, r := range required {
			list[i] = r
		}
		schema["required"] = list
	}
	return schema, nil
}

func (g *generator) collectFields(t reflect.Type, stack []reflect.Type, props map[string]any, required *[]string) error {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := deref(f.Type)
			if ft.Kind() == reflect.Struct {
				if err := g.collectFields(ft, stack, props, required); err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if _, dup := props[name]; dup {
			return fmt.Errorf("%s: duplicate json field %q", typeKey(t), name)
		}
		var schema map[string]any
		var err error
		if strings.Contains(opts, "string") {
			schema = map[string]any{"type": "string"}
		} else {
			schema, err = g.typeSchema(f.Type, stack)
			if err != nil {
				return fmt.Errorf("%s.%s: %w", typeKey(t), f.Name, err)
			}
			// encoding/json writes a nil slice or map as null.
			if k := f.Type.Kind(); (k == reflect.Slice || k == reflect.Map) && f.Type != rawMessageType {
				schema = nullableSchema(schema)
			}
		}
		props[name] = schema
		if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
			*required = append(*required, name)
		}
	}
	return nil
}
