// Package params declares and resolves typed, effective-dated service parameters.
// Get is a generic function because Go does not permit type parameters on methods.
package params

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Scope string

const (
	ScopeGlobal Scope = "GLOBAL"
	ScopeTenant Scope = "TENANT"
	ScopeOrg    Scope = "ORG"
)

type ValueType string

const (
	TypeString  ValueType = "STRING"
	TypeInt     ValueType = "INT"
	TypeDecimal ValueType = "DECIMAL"
	TypeBool    ValueType = "BOOL"
	TypeDate    ValueType = "DATE"
	TypeJSON    ValueType = "JSON"
)

var (
	ErrParamMissing          = errors.New("parameter missing")
	ErrTypeMismatch          = errors.New("parameter type mismatch")
	ErrParamUndeclared       = errors.New("parameter is not declared")
	ErrEffectiveDateRequired = errors.New("effective date is required")
)

// ParamError carries a stable machine-readable error code while preserving
// errors.Is compatibility with the package sentinel.
type ParamError struct {
	Code   string
	Module string
	Param  string
	Cause  error
}

func (e *ParamError) Error() string {
	return fmt.Sprintf("%s: %s.%s: %v", e.Code, e.Module, e.Param, e.Cause)
}
func (e *ParamError) Unwrap() error { return e.Cause }

type ParamSpec struct {
	Code     string
	Type     ValueType
	Unit     string
	Scope    Scope
	Required bool
}
type CodeSetSpec struct {
	Code  string
	Items []CodeItemSpec
}
type CodeItemSpec struct {
	Code       string
	ParentCode string
	Name       string
	Sort       int
	Active     bool
}
type ModuleSpec struct {
	Name     string
	Params   []ParamSpec
	CodeSets []CodeSetSpec
}
type ScopeKey struct {
	TenantID      string
	OrgCode       string
	EffectiveDate time.Time
}
type Registry struct {
	client  Resolver
	modules map[string]ModuleSpec
	mu      sync.Mutex
	cache   map[string]cacheEntry
}

// Resolver is implemented by the platform-service gRPC client.
type Resolver interface {
	ResolveParameter(context.Context, *platformv1.ResolveParameterRequest) (*platformv1.Parameter, error)
}

type cacheEntry struct {
	value     any
	expiresAt time.Time
}

const cacheTTL = 30 * time.Second

func NewRegistry(client Resolver) *Registry {
	return &Registry{client: client, modules: map[string]ModuleSpec{}, cache: map[string]cacheEntry{}}
}

func (r *Registry) Declare(spec ModuleSpec) error {
	if r == nil || r.client == nil {
		return errors.New("platform parameter resolver is required")
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		return errors.New("parameter module is required")
	}
	if _, exists := r.modules[spec.Name]; exists {
		return fmt.Errorf("module %q already declared", spec.Name)
	}
	seen := map[string]bool{}
	for _, p := range spec.Params {
		if p.Code == "" || !validScope(p.Scope) || !validType(p.Type) {
			return fmt.Errorf("invalid parameter declaration in module %s", spec.Name)
		}
		key := p.Code + "/" + string(p.Scope)
		if seen[key] {
			return fmt.Errorf("duplicate parameter %s", key)
		}
		seen[key] = true
	}
	for _, set := range spec.CodeSets {
		if set.Code == "" {
			return errors.New("code set is missing code")
		}
	}
	r.modules[spec.Name] = spec
	return nil
}

func validType(t ValueType) bool {
	switch t {
	case TypeString, TypeInt, TypeDecimal, TypeBool, TypeDate, TypeJSON:
		return true
	}
	return false
}
func validScope(s Scope) bool { return s == ScopeGlobal || s == ScopeTenant || s == ScopeOrg }

// Verify fails startup when declared required values or declared catalogue rows are absent/mismatched.
func (r *Registry) Verify(ctx context.Context) error {
	var problems []string
	effectiveDate := time.Now().UTC().Format("2006-01-02")
	for module, spec := range r.modules {
		for _, p := range spec.Params {
			if !p.Required {
				continue
			}
			resolved, err := r.client.ResolveParameter(ctx, &platformv1.ResolveParameterRequest{
				Module: module, Key: p.Code, EffectiveDate: effectiveDate,
				Scopes: []*platformv1.ScopeSelector{{ScopeType: "global"}},
			})
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s.%s (%s, %s): %v", module, p.Code, p.Type, p.Scope, err))
				continue
			}
			if resolved == nil {
				problems = append(problems, fmt.Sprintf("%s.%s: platform resolver returned an empty response", module, p.Code))
				continue
			}
			if !strings.EqualFold(resolved.GetValueType(), string(p.Type)) || resolved.GetUnit() != p.Unit {
				problems = append(problems, fmt.Sprintf("%s.%s (%s, %s)", module, p.Code, p.Type, p.Scope))
			}
		}
		for _, set := range spec.CodeSets {
			problems = append(problems, "code-set verification is not available through the platform resolver: "+set.Code)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("required parameter registry entries missing: %s", strings.Join(problems, ", "))
	}
	return nil
}

// Get resolves ORG > TENANT > GLOBAL for one effective date and enforces the declared type.
func Get[T any](ctx context.Context, r *Registry, module, code string, key ScopeKey) (T, error) {
	var zero T
	if key.EffectiveDate.IsZero() {
		return zero, &ParamError{Code: "PARAM_EFFECTIVE_DATE_REQUIRED", Module: module, Param: code, Cause: ErrEffectiveDateRequired}
	}
	var declared *ParamSpec
	if r != nil {
		if spec, ok := r.modules[module]; ok {
			for i := range spec.Params {
				if spec.Params[i].Code == code {
					declared = &spec.Params[i]
					break
				}
			}
		}
	}
	if declared == nil {
		return zero, &ParamError{Code: "PARAM_UNDECLARED", Module: module, Param: code, Cause: ErrParamUndeclared}
	}
	if r == nil || r.client == nil {
		return zero, errors.New("platform parameter resolver is required")
	}
	cacheKey := fmt.Sprintf("%s|%s|%s|%s|%s", module, code, key.TenantID, key.OrgCode, key.EffectiveDate.Format("2006-01-02"))
	r.mu.Lock()
	entry, cached := r.cache[cacheKey]
	if cached && time.Now().Before(entry.expiresAt) {
		r.mu.Unlock()
		value, ok := entry.value.(T)
		if ok {
			return value, nil
		}
	}
	r.mu.Unlock()
	scopes := make([]*platformv1.ScopeSelector, 0, 3)
	if declared.Scope == ScopeOrg && key.OrgCode != "" {
		scopes = append(scopes, &platformv1.ScopeSelector{TenantId: key.TenantID, ScopeType: "org", ScopeId: key.OrgCode})
	}
	if declared.Scope != ScopeGlobal && key.TenantID != "" {
		scopes = append(scopes, &platformv1.ScopeSelector{TenantId: key.TenantID, ScopeType: "tenant"})
	}
	scopes = append(scopes, &platformv1.ScopeSelector{ScopeType: "global"})
	resolved, err := r.client.ResolveParameter(ctx, &platformv1.ResolveParameterRequest{TenantId: key.TenantID, Module: module, Key: code, EffectiveDate: key.EffectiveDate.Format("2006-01-02"), Scopes: scopes})
	if err != nil {
		// Missing is a business/configuration error; transport/auth failures remain intact.
		if status.Code(err) == codes.NotFound {
			return zero, &ParamError{Code: "PARAM_MISSING", Module: module, Param: code, Cause: ErrParamMissing}
		}
		return zero, err
	}
	if resolved == nil {
		return zero, errors.New("platform resolver returned an empty response")
	}
	typ, raw := ValueType(strings.ToUpper(resolved.GetValueType())), resolved.GetValue()
	if resolved.GetModule() != "" && resolved.GetModule() != module || resolved.GetKey() != "" && resolved.GetKey() != code {
		return zero, &ParamError{Code: "PARAM_IDENTITY_MISMATCH", Module: module, Param: code, Cause: errors.New("platform resolver returned a different parameter")}
	}
	if typ != declared.Type {
		return zero, &ParamError{Code: "PARAM_TYPE_MISMATCH", Module: module, Param: code, Cause: ErrTypeMismatch}
	}
	var value any
	switch typ {
	case TypeString:
		value = raw
	case TypeInt:
		value, err = strconv.ParseInt(raw, 10, 64)
	case TypeDecimal:
		var parsed float64
		parsed, err = strconv.ParseFloat(raw, 64)
		value = parsed
	case TypeBool:
		value, err = strconv.ParseBool(raw)
	case TypeDate:
		value, err = time.Parse("2006-01-02", raw)
	case TypeJSON:
		var decoded any
		err = json.Unmarshal([]byte(raw), &decoded)
		value = decoded
	}
	if err != nil {
		return zero, fmt.Errorf("parse %s.%s as %s: %w", module, code, typ, err)
	}
	converted, ok := value.(T)
	destinationType := reflect.TypeOf(zero)
	if !ok && value != nil && destinationType != nil && reflect.TypeOf(value).ConvertibleTo(destinationType) {
		converted = reflect.ValueOf(value).Convert(destinationType).Interface().(T)
		ok = true
	}
	if !ok {
		return zero, &ParamError{Code: "PARAM_TYPE_MISMATCH", Module: module, Param: code, Cause: ErrTypeMismatch}
	}
	if resolved.GetUnit() != declared.Unit {
		return zero, &ParamError{Code: "PARAM_UNIT_MISMATCH", Module: module, Param: code, Cause: fmt.Errorf("expected unit %q, got %q", declared.Unit, resolved.GetUnit())}
	}
	r.mu.Lock()
	r.cache[cacheKey] = cacheEntry{value: converted, expiresAt: time.Now().Add(cacheTTL)}
	r.mu.Unlock()
	return converted, nil
}

// RenderGoConstants emits deterministic string constants from a declared code set.
func RenderGoConstants(pkg string, sets []CodeSetSpec) ([]byte, error) {
	if pkg == "" {
		return nil, errors.New("package name is required")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by arda-params. DO NOT EDIT.\npackage %s\n\n", pkg)
	for _, set := range sets {
		fmt.Fprintf(&b, "const (\n")
		for _, item := range set.Items {
			name := goIdent(set.Code + "_" + item.Code)
			fmt.Fprintf(&b, "\t%s = %q\n", name, item.Code)
		}
		fmt.Fprintf(&b, ")\n\n")
	}
	return []byte(b.String()), nil
}
func goIdent(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			if upper {
				r -= 32
			}
			b.WriteRune(r)
			upper = false
		} else if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			upper = false
		} else {
			upper = true
		}
	}
	return b.String()
}
