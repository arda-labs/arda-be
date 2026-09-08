package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
)

// PostingServer implements arda.finance.v1.PostingService (contract v0.2).
// Tenant scope arrives via propagated gRPC metadata (X-Tenant-Id) — the
// same convention as the loan command server.
type PostingServer struct {
	financev1.UnimplementedPostingServiceServer
	posting *service.PostingService
}

func NewPostingServer(posting *service.PostingService) *PostingServer {
	return &PostingServer{posting: posting}
}

func tenantFromContext(ctx context.Context) (string, error) {
	md := ardametadata.FromIncoming(ctx)
	tenantID := strings.TrimSpace(md.TenantID)
	if tenantID == "" {
		return "", fmt.Errorf("tenant scope is required")
	}
	return tenantID, nil
}

func (s *PostingServer) ValidatePosting(ctx context.Context, req *financev1.PostingRequest) (*financev1.ValidationResult, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	result, err := s.posting.ValidatePosting(ctx, tenantID, req)
	if err != nil {
		slog.Warn("posting grpc: validate failed", "err", err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return result, nil
}

func (s *PostingServer) PostTransaction(ctx context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	resp, err := s.posting.PostTransaction(ctx, tenantID, req)
	if err != nil {
		slog.Warn("posting grpc: post failed", "docType", req.GetBusinessReference().GetDocumentType(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return resp, nil
}

func (s *PostingServer) ReverseTransaction(ctx context.Context, req *financev1.ReverseRequest) (*financev1.PostingResponse, error) {
	resp, err := s.posting.ReverseTransaction(ctx, req)
	if err != nil {
		slog.Warn("posting grpc: reverse failed", "entryId", req.GetJournalEntryId(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return resp, nil
}

// GetJournalEntry serves the cancellation flow's init/validate guards: the
// workers check status + reversed_by_entry_id before reversing. Unknown or
// non-readable (PENDING/VOID) entries map to NotFound.
func (s *PostingServer) GetJournalEntry(ctx context.Context, req *financev1.GetJournalEntryRequest) (*financev1.JournalEntryDetail, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	detail, err := s.posting.GetJournalEntry(ctx, tenantID, req.GetEntryNo(), req.GetEntryId())
	if err != nil {
		slog.Warn("posting grpc: get journal entry failed", "entryNo", req.GetEntryNo(), "err", err)
		if errors.Is(err, service.ErrJournalEntryNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return detail, nil
}

func (s *PostingServer) ReservePosting(ctx context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	resp, err := s.posting.ReservePosting(ctx, tenantID, req)
	if err != nil {
		slog.Warn("posting grpc: reserve failed", "docType", req.GetBusinessReference().GetDocumentType(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return resp, nil
}

func (s *PostingServer) ReleasePosting(ctx context.Context, req *financev1.ReleaseRequest) (*financev1.PostingResponse, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	resp, err := s.posting.ReleasePosting(ctx, tenantID, req)
	if err != nil {
		slog.Warn("posting grpc: release failed", "entryId", req.GetJournalEntryId(), "err", err)
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return resp, nil
}
