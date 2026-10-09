package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-docno"
)

var (
	ErrDocumentNumberNotFound  = errors.New("document number not found")
	ErrRenumberRequestNotFound = errors.New("renumber request not found")
	ErrRenumberMakerChecker    = errors.New("maker and checker must be different users")
	ErrRenumberClosedPeriod    = errors.New("renumbering a closed period requires locked-period permission")
)

type DocumentNumberService struct{ db *sql.DB }

type RenumberRequest struct {
	ID               string `json:"id"`
	DocumentID       string `json:"document_id"`
	PreviousDisplay  string `json:"previous_display_no"`
	RequestedDisplay string `json:"requested_display_no"`
	Reason           string `json:"reason"`
	Status           string `json:"status"`
}

func NewDocumentNumberService(db *sql.DB) *DocumentNumberService {
	return &DocumentNumberService{db: db}
}

func (s *DocumentNumberService) RequestRenumber(ctx context.Context, tenantID, documentID, makerID, displayNo, reason string) (*RenumberRequest, error) {
	if tenantID == "" || documentID == "" || makerID == "" || strings.TrimSpace(displayNo) == "" || strings.TrimSpace(reason) == "" {
		return nil, errors.New("tenant, document, actor, target number, and reason are required")
	}
	displayNo = strings.TrimSpace(displayNo)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT display_no FROM document_number WHERE tenant_id=$1 AND document_id=$2 AND status='ACTIVE' FOR UPDATE`, tenantID, documentID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDocumentNumberNotFound
	}
	if err != nil {
		return nil, err
	}
	if current == displayNo {
		return nil, errors.New("requested number is unchanged")
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO doc_number_change (tenant_id, document_id, previous_display_no, requested_display_no, reason, maker_user_id) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id::text`, tenantID, documentID, current, displayNo, strings.TrimSpace(reason), makerID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create renumber request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RenumberRequest{ID: id, DocumentID: documentID, PreviousDisplay: current, RequestedDisplay: displayNo, Reason: strings.TrimSpace(reason), Status: "PENDING"}, nil
}

func (s *DocumentNumberService) ApproveRenumber(ctx context.Context, tenantID, requestID, checkerID string, allowClosedPeriod bool) (*RenumberRequest, error) {
	if tenantID == "" || requestID == "" || checkerID == "" {
		return nil, errors.New("tenant, request, and checker are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var req RenumberRequest
	var maker string
	err = tx.QueryRowContext(ctx, `SELECT id::text, document_id, previous_display_no, requested_display_no, reason, status, maker_user_id FROM doc_number_change WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, requestID).Scan(&req.ID, &req.DocumentID, &req.PreviousDisplay, &req.RequestedDisplay, &req.Reason, &req.Status, &maker)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRenumberRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	if req.Status != "PENDING" {
		return nil, errors.New("renumber request is not pending")
	}
	if maker == checkerID {
		return nil, ErrRenumberMakerChecker
	}
	var businessDate string
	var current string
	err = tx.QueryRowContext(ctx, `SELECT display_no, business_date::text FROM document_number WHERE tenant_id=$1 AND document_id=$2 AND status='ACTIVE' FOR UPDATE`, tenantID, req.DocumentID).Scan(&current, &businessDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDocumentNumberNotFound
	}
	if err != nil {
		return nil, err
	}
	if current != req.PreviousDisplay {
		return nil, errors.New("document number changed after request; create a new request")
	}
	var periodStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM fin_periods WHERE tenant_id=$1 AND $2::date BETWEEN start_date AND end_date FOR SHARE`, tenantID, businessDate).Scan(&periodStatus)
	if err != nil {
		return nil, fmt.Errorf("resolve accounting period: %w", err)
	}
	if periodStatus != "OPEN" && !allowClosedPeriod {
		return nil, ErrRenumberClosedPeriod
	}
	if periodStatus != "OPEN" && strings.TrimSpace(req.Reason) == "" {
		return nil, errors.New("reason is required for a closed period")
	}
	var aliasExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM doc_number_alias WHERE tenant_id=$1 AND display_no=$2)`, tenantID, req.RequestedDisplay).Scan(&aliasExists); err != nil {
		return nil, err
	}
	if aliasExists {
		return nil, fmt.Errorf("%w: requested number is already active or retired", docno.ErrDocumentNumberExists)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO retired_no (tenant_id, display_no, document_id) VALUES ($1,$2,$3)`, tenantID, current, req.DocumentID); err != nil {
		return nil, fmt.Errorf("retire previous number: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE doc_number_alias SET status='RETIRED' WHERE tenant_id=$1 AND display_no=$2 AND document_id=$3 AND status='ACTIVE'`, tenantID, current, req.DocumentID)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return nil, errors.New("active display-number alias missing")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO doc_number_alias (tenant_id, display_no, document_id, status) VALUES ($1,$2,$3,'ACTIVE')`, tenantID, req.RequestedDisplay, req.DocumentID); err != nil {
		return nil, fmt.Errorf("reserve replacement number: %w", err)
	}
	result, err = tx.ExecContext(ctx, `UPDATE document_number SET display_no=$3 WHERE tenant_id=$1 AND document_id=$2`, tenantID, req.DocumentID, req.RequestedDisplay)
	if err != nil {
		return nil, fmt.Errorf("set replacement number: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return nil, ErrDocumentNumberNotFound
	}
	_, err = tx.ExecContext(ctx, `UPDATE doc_number_change SET status='APPROVED', checker_user_id=$3, checked_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, requestID, checkerID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	req.Status = "APPROVED"
	return &req, nil
}
