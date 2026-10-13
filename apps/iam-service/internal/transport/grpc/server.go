package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
)

type UserServiceServer struct {
	iamv1.UnimplementedUserServiceServer
	userRepo   *repository.UserRepository
	tenantRepo *repository.TenantRepository
}

func NewUserServiceServer(userRepo *repository.UserRepository, tenantRepo *repository.TenantRepository) *UserServiceServer {
	return &UserServiceServer{userRepo: userRepo, tenantRepo: tenantRepo}
}

func (s *UserServiceServer) ListActiveTenants(ctx context.Context, _ *iamv1.ListActiveTenantsRequest) (*iamv1.ListActiveTenantsResponse, error) {
	ids, err := s.tenantRepo.ListActiveTenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	return &iamv1.ListActiveTenantsResponse{TenantIds: ids}, nil
}

func (s *UserServiceServer) GetUserBatch(ctx context.Context, req *iamv1.GetUserBatchRequest) (*iamv1.GetUserBatchResponse, error) {
	users, err := s.userRepo.GetUsersByIDs(ctx, req.UserIds)
	if err != nil {
		return nil, err
	}

	infos := make([]*iamv1.UserInfo, 0, len(users))
	for _, u := range users {
		name := u.DisplayName
		if name == "" {
			name = u.FirstName + " " + u.LastName
			if name == " " {
				name = u.Username
			}
		}
		avatar := u.PictureURL
		if avatar == "" && u.AvatarFileID != "" {
			avatar = u.AvatarFileID
		}
		infos = append(infos, &iamv1.UserInfo{
			Id:         u.ID,
			Name:       name,
			Email:      u.Email,
			AvatarUrl:  avatar,
			FirstName:  u.FirstName,
			LastName:   u.LastName,
			Department: u.Department,
			Title:      u.Position,
		})
	}
	return &iamv1.GetUserBatchResponse{Users: infos}, nil
}

func (s *UserServiceServer) ResolveNotificationRecipients(ctx context.Context, req *iamv1.ResolveNotificationRecipientsRequest) (*iamv1.ResolveNotificationRecipientsResponse, error) {
	tenantID := strings.TrimSpace(ardametadata.FromIncoming(ctx).TenantID)
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant scope is required")
	}
	if len(req.GetUserIds()) > 200 || len(req.GetGroupIds()) > 200 || len(req.GetRoleCodes()) > 200 {
		return nil, status.Error(codes.InvalidArgument, "recipient selectors are limited to 200 values per type")
	}
	ids, err := s.userRepo.ResolveNotificationRecipientIDs(ctx, tenantID, req.GetUserIds(), req.GetGroupIds(), req.GetRoleCodes())
	if err != nil {
		return nil, status.Error(codes.Internal, "resolve notification recipients failed")
	}
	return &iamv1.ResolveNotificationRecipientsResponse{UserIds: ids}, nil
}

// Dependencies is everything the IAM gRPC surface serves from.
type Dependencies struct {
	UserRepo   *repository.UserRepository
	TenantRepo *repository.TenantRepository
	// Gateway surface (GatewayService); see gateway.go.
	Users    UserContexts
	Sessions Sessions
	MFA      MFA
	Audit    Audit
}

// sourcePolicy builds the per-method caller allowlist: UserService keeps its
// historical callers, ListActiveTenants is platform-only, and every
// GatewayService method is callable by auth-gateway alone. Deriving the list
// from the service descriptor means a new gateway RPC cannot be forgotten.
func sourcePolicy() (map[string]struct{}, map[string]map[string]struct{}) {
	defaultSources := map[string]struct{}{"workflow-service": {}, "notification-service": {}}
	methodSources := map[string]map[string]struct{}{
		"/arda.iam.v1.UserService/ListActiveTenants": {"platform-service": {}},
	}
	for _, m := range iamv1.GatewayService_ServiceDesc.Methods {
		methodSources["/"+iamv1.GatewayService_ServiceDesc.ServiceName+"/"+m.MethodName] = map[string]struct{}{gatewaySource: {}}
	}
	return defaultSources, methodSources
}

// newServer builds the gRPC server with the production interceptor chain.
// creds may be nil only for in-memory tests; ListenAndServe always passes
// mTLS credentials.
func newServer(serviceSecret string, creds credentials.TransportCredentials, deps Dependencies) *grpc.Server {
	defaultSources, methodSources := sourcePolicy()
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			interceptors.UnaryServerRecovery(slog.Default()),
			interceptors.UnaryServerServiceAuthMethodSources(serviceSecret, "iam-service", defaultSources, methodSources),
		),
	}
	if creds != nil {
		opts = append(opts, grpc.Creds(creds))
	}
	srv := grpc.NewServer(opts...)
	if deps.UserRepo != nil || deps.TenantRepo != nil {
		iamv1.RegisterUserServiceServer(srv, NewUserServiceServer(deps.UserRepo, deps.TenantRepo))
	}
	if deps.Users != nil && deps.Sessions != nil && deps.MFA != nil {
		iamv1.RegisterGatewayServiceServer(srv, NewGatewayServer(deps.Users, deps.Sessions, deps.MFA, deps.Audit))
	}
	return srv
}

func ListenAndServe(grpcAddr string, deps Dependencies) (*grpc.Server, error) {
	if deps.Users == nil || deps.Sessions == nil || deps.MFA == nil {
		return nil, fmt.Errorf("gateway service dependencies are required")
	}
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("listen grpc: %w", err)
	}
	serviceSecret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, fmt.Errorf("service identity is not configured: %w", err)
	}
	transportCreds, err := identity.ServerTransportCredentials()
	if err != nil {
		return nil, fmt.Errorf("grpc tls is not configured: %w", err)
	}
	srv := newServer(serviceSecret, transportCreds, deps)
	go func() {
		if err := srv.Serve(lis); err != nil {
			// The listener is owned by this long-running process; a serve failure
			// must be visible instead of being silently discarded.
			fmt.Fprintln(os.Stderr, "iam grpc serve failed:", err)
		}
	}()
	return srv, nil
}
