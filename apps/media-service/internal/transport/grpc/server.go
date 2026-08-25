package grpc

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/media-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	mediav1 "github.com/arda-labs/arda/libs/go/arda-proto/media/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MediaServer struct {
	mediav1.UnimplementedMediaServiceServer
	service *service.MediaService
}

func NewMediaServer(mediaService *service.MediaService) *MediaServer {
	return &MediaServer{service: mediaService}
}

func (s *MediaServer) AttachFiles(ctx context.Context, req *mediav1.AttachFilesRequest) (*mediav1.AttachFilesResponse, error) {
	if s == nil || s.service == nil {
		return nil, status.Error(codes.FailedPrecondition, "media service is not configured")
	}
	if req == nil || len(req.GetPublicIds()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "public_ids are required")
	}
	if strings.TrimSpace(req.GetOwnerType()) == "" || strings.TrimSpace(req.GetOwnerId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_type and owner_id are required")
	}
	metadata := ardametadata.FromIncoming(ctx)
	tenantID := strings.TrimSpace(metadata.TenantID)
	orgID := strings.TrimSpace(metadata.OrgID)
	userID := strings.TrimSpace(metadata.ActorUserID)
	if userID == "" {
		userID = strings.TrimSpace(metadata.UserID)
	}
	if tenantID == "" || orgID == "" || userID == "" {
		return nil, status.Error(codes.PermissionDenied, "verified tenant, organization and actor scope are required")
	}
	if err := s.service.AttachFiles(ctx, req.GetPublicIds(), tenantID, orgID, userID, req.GetOwnerType(), req.GetOwnerId()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &mediav1.AttachFilesResponse{Ok: true}, nil
}
