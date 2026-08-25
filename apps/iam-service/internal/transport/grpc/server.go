package grpc

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	"google.golang.org/grpc"

	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	iamv1 "github.com/arda-labs/arda/libs/go/arda-proto/iam/v1"
)

type UserServiceServer struct {
	iamv1.UnimplementedUserServiceServer
	userRepo *repository.UserRepository
}

func NewUserServiceServer(userRepo *repository.UserRepository) *UserServiceServer {
	return &UserServiceServer{userRepo: userRepo}
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

func ListenAndServe(grpcAddr string, userRepo *repository.UserRepository) (*grpc.Server, error) {
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
	srv := grpc.NewServer(
		grpc.Creds(transportCreds),
		grpc.ChainUnaryInterceptor(
			interceptors.UnaryServerServiceAuth(serviceSecret, "iam-service", map[string]struct{}{"workflow-service": {}}),
		),
	)
	iamv1.RegisterUserServiceServer(srv, NewUserServiceServer(userRepo))
	go func() {
		if err := srv.Serve(lis); err != nil {
			// The listener is owned by this long-running process; a serve failure
			// must be visible instead of being silently discarded.
			fmt.Fprintln(os.Stderr, "iam grpc serve failed:", err)
		}
	}()
	return srv, nil
}
