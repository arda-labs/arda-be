// Package params declares and resolves typed, effective-dated service parameters.
// Get is a generic function because Go does not permit type parameters on methods.
package params

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
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
	db      *sql.DB
	modules map[string]ModuleSpec
}

func NewRegistry(db *sql.DB) *Registry { return &Registry{db: db, modules: map[string]ModuleSpec{}} }

func (r *Registry) Declare(spec ModuleSpec) error {
	if r == nil || r.db == nil {
		return errors.New("parameter registry database is required")
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
	for module, spec := range r.modules {
		for _, p := range spec.Params {
			if !p.Required {
				continue
			}
			var found bool
			err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM parameter WHERE module=$1 AND code=$2 AND value_type=$3 AND unit=$4 AND effective_to IS NULL AND CASE scope WHEN 'ORG' THEN 1 WHEN 'TENANT' THEN 2 ELSE 3 END >= CASE $5 WHEN 'ORG' THEN 1 WHEN 'TENANT' THEN 2 ELSE 3 END)`, module, p.Code, p.Type, p.Unit, p.Scope).Scan(&found)
			if err != nil {
				return fmt.Errorf("verify parameter %s.%s: %w", module, p.Code, err)
			}
			if !found {
				problems = append(problems, fmt.Sprintf("%s.%s (%s, %s)", module, p.Code, p.Type, p.Scope))
			}
		}
		for _, set := range spec.CodeSets {
			var found bool
			err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM code_set WHERE code=$1)`, set.Code).Scan(&found)
			if err != nil {
				return fmt.Errorf("verify code set %s: %w", set.Code, err)
			}
			if !found {
				problems = append(problems, "code set "+set.Code)
				continue
			}
			for _, item := range set.Items {
				var itemFound bool
				err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM code_item WHERE set_code=$1 AND code=$2 AND active=$3 AND parent_code IS NOT DISTINCT FROM NULLIF($4,''))`, set.Code, item.Code, item.Active, item.ParentCode).Scan(&itemFound)
				if err != nil {
					return fmt.Errorf("verify code item %s.%s: %w", set.Code, item.Code, err)
				}
				if !itemFound {
					problems = append(problems, fmt.Sprintf("code item %s.%s", set.Code, item.Code))
				}
			}
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
	var typ ValueType
	var raw string
	err := r.db.QueryRowContext(ctx, `SELECT value_type,value FROM parameter WHERE module=$1 AND code=$2 AND effective_from <= $3::date AND (effective_to IS NULL OR effective_to >= $3::date) AND CASE scope WHEN 'ORG' THEN 1 WHEN 'TENANT' THEN 2 ELSE 3 END >= CASE $6 WHEN 'ORG' THEN 1 WHEN 'TENANT' THEN 2 ELSE 3 END AND ((scope='ORG' AND tenant_id=$4 AND org_code=$5 AND $5<>'') OR (scope='TENANT' AND tenant_id=$4) OR (scope='GLOBAL' AND tenant_id IS NULL)) ORDER BY CASE scope WHEN 'ORG' THEN 1 WHEN 'TENANT' THEN 2 ELSE 3 END,effective_from DESC LIMIT 1`, module, code, key.EffectiveDate.Format("2006-01-02"), nullIfEmpty(key.TenantID), key.OrgCode, declared.Scope).Scan(&typ, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, &ParamError{Code: "PARAM_MISSING", Module: module, Param: code, Cause: ErrParamMissing}
	}
	if err != nil {
		return zero, err
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
	return converted, nil
}
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
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
