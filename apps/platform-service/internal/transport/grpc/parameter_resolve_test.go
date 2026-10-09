package grpc

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestLoanCanResolveOnlyGlobalParametersWithoutTenant(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	_, err := db.Exec(`INSERT INTO plt_system_parameters (id,module,key,value,value_type,unit,scope_type,effective_from) VALUES ('parameter-global','loan','LNM_RATE','0.75','decimal','percent','global',DATE '0001-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewPlatformServer(service.NewPlatformService(repository.NewPlatformRepository(db)))
	secret := "service-test-secret"
	interceptor := interceptors.UnaryServerServiceAuthMethodSources(secret, "platform-service", map[string]struct{}{}, map[string]map[string]struct{}{
		"/arda.platform.v1.PlatformService/ResolveParameter": {"loan-service": {}},
	})
	call := func(req *platformv1.ResolveParameterRequest) (*platformv1.Parameter, error) {
		token, err := identity.Issue(secret, "loan-service", "platform-service", time.Now(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		var response any
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identity.MetadataKey, token))
		_, err = interceptor(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/arda.platform.v1.PlatformService/ResolveParameter"}, func(ctx context.Context, request any) (any, error) {
			response, err = srv.ResolveParameter(ctx, request.(*platformv1.ResolveParameterRequest))
			return response, err
		})
		if err != nil {
			return nil, err
		}
		return response.(*platformv1.Parameter), nil
	}
	base := &platformv1.ResolveParameterRequest{Module: "loan", Key: "LNM_RATE", EffectiveDate: "2026-10-08", Scopes: []*platformv1.ScopeSelector{{ScopeType: "global"}}}
	got, err := call(base)
	if err != nil || got.GetValue() != "0.75" {
		t.Fatalf("global resolve = %+v, %v", got, err)
	}
	base.Scopes = []*platformv1.ScopeSelector{{ScopeType: "org", ScopeId: "BR-01"}}
	if _, err := call(base); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("org scope without tenant error = %v", err)
	}
}
