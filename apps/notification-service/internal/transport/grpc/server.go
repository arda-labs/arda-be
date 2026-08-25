package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	notificationv1 "github.com/arda-labs/arda/libs/go/arda-proto/notification/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	notificationv1.UnimplementedNotificationServiceServer
	svc *service.NotificationService
}

func NewServer(svc *service.NotificationService) *Server { return &Server{svc: svc} }

func (s *Server) AcceptNotification(ctx context.Context, req *notificationv1.AcceptNotificationRequest) (*notificationv1.AcceptNotificationResponse, error) {
	claims, ok := interceptors.ServiceClaims(ctx)
	if !ok || strings.TrimSpace(claims.Source) == "" {
		return nil, status.Error(codes.Unauthenticated, "verified service identity is required")
	}
	if strings.TrimSpace(req.GetSourceService()) != claims.Source {
		return nil, status.Error(codes.PermissionDenied, "source service does not match workload identity")
	}
	var recipients []domain.Recipient
	if err := json.Unmarshal([]byte(req.GetRecipientsJson()), &recipients); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid recipients")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(req.GetPayloadJson()), &payload); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid payload")
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(req.GetParamsJson()), &params); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid params")
	}
	n, err := s.svc.Accept(ctx, service.AcceptInput{
		TenantID: req.GetTenantId(), IdempotencyKey: req.GetIdempotencyKey(), SourceService: req.GetSourceService(),
		SourceEventID: req.GetSourceEventId(), EventType: req.GetEventType(), TemplateKey: req.GetTemplateKey(),
		Channels: req.GetChannels(), Recipients: recipients, Payload: payload, CorrelationID: req.GetCorrelationId(),
		Priority: int(req.GetPriority()), Type: req.GetType(), TitleKey: req.GetTitleKey(), BodyKey: req.GetBodyKey(),
		Href: req.GetHref(), Params: params,
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("accept notification: %v", err))
	}
	return &notificationv1.AcceptNotificationResponse{NotificationId: n.PublicID, Status: n.Status}, nil
}
