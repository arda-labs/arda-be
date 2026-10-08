package params

import (
	"context"
	"errors"
	"testing"
	"time"

	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeResolver struct {
	parameter *platformv1.Parameter
	err       error
	calls     int
	request   *platformv1.ResolveParameterRequest
}

func (f *fakeResolver) ResolveParameter(_ context.Context, req *platformv1.ResolveParameterRequest) (*platformv1.Parameter, error) {
	f.calls++
	f.request = req
	return f.parameter, f.err
}

func TestGetUsesPlatformScopesEffectiveDateAndCaches(t *testing.T) {
	resolver := &fakeResolver{parameter: &platformv1.Parameter{Module: "loan", Key: "LNM_RATE", Value: "0.75", ValueType: "decimal", Unit: "percent", ScopeType: "global"}}
	registry := NewRegistry(resolver)
	if err := registry.Declare(ModuleSpec{Name: "loan", Params: []ParamSpec{{Code: "LNM_RATE", Type: TypeDecimal, Unit: "percent", Scope: ScopeOrg, Required: true}}}); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	key := ScopeKey{TenantID: "tenant-1", OrgCode: "BR-01", EffectiveDate: date}
	got, err := Get[float64](context.Background(), registry, "loan", "LNM_RATE", key)
	if err != nil || got != .75 {
		t.Fatalf("Get() = %v, %v", got, err)
	}
	_, err = Get[float64](context.Background(), registry, "loan", "LNM_RATE", key)
	if err != nil || resolver.calls != 1 {
		t.Fatalf("second Get() calls=%d err=%v", resolver.calls, err)
	}
	if resolver.request.GetEffectiveDate() != "2026-10-08" {
		t.Fatalf("effective date = %q", resolver.request.GetEffectiveDate())
	}
	if len(resolver.request.GetScopes()) != 3 || resolver.request.GetScopes()[0].GetScopeType() != "org" || resolver.request.GetScopes()[1].GetScopeType() != "tenant" || resolver.request.GetScopes()[2].GetScopeType() != "global" {
		t.Fatalf("scope order = %v", resolver.request.GetScopes())
	}
}

func TestGetFailsClosedForMissingTypeAndUnit(t *testing.T) {
	ctx := context.Background()
	date := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	key := ScopeKey{EffectiveDate: date}
	newRegistry := func(resolver *fakeResolver) *Registry {
		r := NewRegistry(resolver)
		if err := r.Declare(ModuleSpec{Name: "loan", Params: []ParamSpec{{Code: "RATE", Type: TypeDecimal, Unit: "percent", Scope: ScopeGlobal}}}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	missing := newRegistry(&fakeResolver{err: status.Error(codes.NotFound, "missing")})
	if _, err := Get[float64](ctx, missing, "loan", "RATE", key); !errors.Is(err, ErrParamMissing) {
		t.Fatalf("missing error = %v", err)
	}
	wrongType := newRegistry(&fakeResolver{parameter: &platformv1.Parameter{Value: "1", ValueType: "string", Unit: "percent"}})
	if _, err := Get[float64](ctx, wrongType, "loan", "RATE", key); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("type error = %v", err)
	}
	wrongUnit := newRegistry(&fakeResolver{parameter: &platformv1.Parameter{Value: "1", ValueType: "decimal", Unit: "fraction"}})
	if _, err := Get[float64](ctx, wrongUnit, "loan", "RATE", key); err == nil {
		t.Fatal("unit mismatch must fail")
	}
}

func TestVerifyUsesGlobalPlatformParameter(t *testing.T) {
	resolver := &fakeResolver{parameter: &platformv1.Parameter{Module: "loan", Key: "RATE", Value: ".75", ValueType: "decimal", Unit: "percent"}}
	r := NewRegistry(resolver)
	if err := r.Declare(ModuleSpec{Name: "loan", Params: []ParamSpec{{Code: "RATE", Type: TypeDecimal, Unit: "percent", Scope: ScopeOrg, Required: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resolver.request.GetTenantId() != "" || len(resolver.request.GetScopes()) != 1 || resolver.request.GetScopes()[0].GetScopeType() != "global" {
		t.Fatalf("startup verify must request global only: %+v", resolver.request)
	}
}
