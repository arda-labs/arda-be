package loan

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
	"google.golang.org/grpc"
)

const defaultTimeout = 5 * time.Second

// Kinds enumerates the loan adjustment flow kinds; workflow-service workers
// and loan-service case-types share this list so job types never drift.
var Kinds = []string{
	"debt-change",
	"rate-change",
	"restructure",
	"waiver",
	"writeoff",
	"recovery",
	"fund-check",
	"revenue-allocation",
	"vfu-fee-allocation",
	"off-balance-export",
	"mortgage-adjust",
}

// IsValidKind reports whether kind is a registered loan adjustment flow.
func IsValidKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

type Client struct {
	conn    *grpc.ClientConn
	api     loanv1.LoanCommandServiceClient
	timeout time.Duration
}

func Dial(ctx context.Context, addr, sourceService string, logger *slog.Logger) (*Client, error) {
	_ = ctx
	_ = logger
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("loan grpc address is required")
	}
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return nil, errors.New("loan grpc service identity is not configured: " + err.Error())
	}
	transportCreds, err := identity.ClientTransportCredentials("loan-service")
	if err != nil {
		return nil, errors.New("loan grpc tls is not configured: " + err.Error())
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithChainUnaryInterceptor(
			interceptors.UnaryClientMetadata(sourceService, ardametadata.Context{}),
			interceptors.UnaryClientServiceAuth(secret, sourceService, "loan-service"),
		),
	)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:    conn,
		api:     loanv1.NewLoanCommandServiceClient(conn),
		timeout: defaultTimeout,
	}
	conn.Connect()
	return client, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) UpdateContractStatus(ctx context.Context, contractID, status string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.UpdateContractStatus(callCtx, &loanv1.UpdateContractStatusRequest{
		ContractId: contractID,
		Status:     status,
	})
	return err
}

// GetContract reads the minimal contract brief (status lifecycle) — the
// formation execute step keys off it.
func (c *Client) GetContract(ctx context.Context, contractID string) (*loanv1.ContractBrief, error) {
	if c == nil {
		return nil, errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetContract(callCtx, &loanv1.GetContractRequest{ContractId: contractID})
}

// GetOperationMetrics returns the TT92 performance aggregates (collection
// volume in period + non-performing balance). Tenant is stamped into outgoing
// gRPC metadata since the caller (finance statement run) is not itself a gRPC
// request.
func (c *Client) GetOperationMetrics(ctx context.Context, tenantID, fromDate, toDate string) (*loanv1.GetOperationMetricsResponse, error) {
	if c == nil {
		return nil, errors.New("loan client is nil")
	}
	ctx = ardametadata.AppendToOutgoing(ctx, ardametadata.Context{TenantID: tenantID})
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetOperationMetrics(callCtx, &loanv1.GetOperationMetricsRequest{
		TenantId: tenantID,
		FromDate: fromDate,
		ToDate:   toDate,
	})
}

// CheckFormation validates the contract is actionable (BPMN validate).
func (c *Client) CheckFormation(ctx context.Context, contractID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckFormation(callCtx, &loanv1.CheckFormationRequest{ContractId: contractID})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) CheckAdjustment(ctx context.Context, kind, adjustmentID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckAdjustment(callCtx, &loanv1.CheckAdjustmentRequest{
		Kind:         kind,
		AdjustmentId: adjustmentID,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

func (c *Client) ResolveAdjustment(ctx context.Context, kind, adjustmentID, decision, decidedBy, note string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveAdjustment(callCtx, &loanv1.ResolveAdjustmentRequest{
		Kind:         kind,
		AdjustmentId: adjustmentID,
		Decision:     decision,
		DecidedBy:    decidedBy,
		Note:         note,
	})
	return err
}

// CheckDisbursement validates the disbursement is actionable (BPMN validate).
func (c *Client) CheckDisbursement(ctx context.Context, disbursementID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckDisbursement(callCtx, &loanv1.CheckDisbursementRequest{
		DisbursementId: disbursementID,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// GetDisbursementPostingDetail returns everything the posting step needs.
func (c *Client) GetDisbursementPostingDetail(ctx context.Context, disbursementID string) (*loanv1.DisbursementPostingDetail, error) {
	if c == nil {
		return nil, errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.GetDisbursementPostingDetail(callCtx, &loanv1.GetDisbursementPostingDetailRequest{
		DisbursementId: disbursementID,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// SettleDisbursement marks the disbursement POSTED with its journal entry.
func (c *Client) SettleDisbursement(ctx context.Context, disbursementID, journalEntryID, actor string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.SettleDisbursement(callCtx, &loanv1.SettleDisbursementRequest{
		DisbursementId: disbursementID,
		JournalEntryId: journalEntryID,
		Actor:          actor,
	})
	return err
}

// ResolveDisbursement applies APPROVE/REJECT without posting.
func (c *Client) ResolveDisbursement(ctx context.Context, disbursementID, decision, decidedBy, note string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveDisbursement(callCtx, &loanv1.ResolveDisbursementRequest{
		DisbursementId: disbursementID,
		Decision:       decision,
		DecidedBy:      decidedBy,
		Note:           note,
	})
	return err
}


// CheckCollection validates the collection is actionable (BPMN validate).
func (c *Client) CheckCollection(ctx context.Context, collectionID string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckCollection(callCtx, &loanv1.CheckCollectionRequest{
		CollectionId: collectionID,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// GetCollectionPostingDetail returns everything the posting step needs.
func (c *Client) GetCollectionPostingDetail(ctx context.Context, collectionID string) (*loanv1.CollectionPostingDetail, error) {
	if c == nil {
		return nil, errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetCollectionPostingDetail(callCtx, &loanv1.GetCollectionPostingDetailRequest{
		CollectionId: collectionID,
	})
}

// SettleCollection marks the collection POSTED with its journal entry.
func (c *Client) SettleCollection(ctx context.Context, collectionID, journalEntryID, actor string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.SettleCollection(callCtx, &loanv1.SettleCollectionRequest{
		CollectionId:   collectionID,
		JournalEntryId: journalEntryID,
		Actor:          actor,
	})
	return err
}

// ResolveCollection applies APPROVE/REJECT without posting.
func (c *Client) ResolveCollection(ctx context.Context, collectionID, decision, decidedBy, note string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveCollection(callCtx, &loanv1.ResolveCollectionRequest{
		CollectionId: collectionID,
		Decision:     decision,
		DecidedBy:    decidedBy,
		Note:         note,
	})
	return err
}

// ── Batch flows (iteration 13, 1 hồ sơ — N hợp đồng) ──

// GetBatchPostingDetail returns the batch header + per-row posting context
// (batch_type dispatches DISB_REGISTER / DISB_COMPLETE / COLLECTION).
func (c *Client) GetBatchPostingDetail(ctx context.Context, batchID, batchType string) (*loanv1.BatchPostingDetail, error) {
	if c == nil {
		return nil, errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.api.GetBatchPostingDetail(callCtx, &loanv1.GetBatchRequest{
		BatchId:   batchID,
		BatchType: batchType,
	})
}

// CheckBatch re-validates the batch is actionable (BPMN validate).
func (c *Client) CheckBatch(ctx context.Context, batchID, batchType string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckBatch(callCtx, &loanv1.CheckBatchRequest{
		BatchId:   batchID,
		BatchType: batchType,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// SettleBatch marks the batch POSTED and applies the per-row side effects
// (loan-service loops its existing per-row settle semantics).
func (c *Client) SettleBatch(ctx context.Context, batchID, batchType, journalEntryID, actor string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.SettleBatch(callCtx, &loanv1.SettleBatchRequest{
		BatchId:        batchID,
		BatchType:      batchType,
		JournalEntryId: journalEntryID,
		Actor:          actor,
	})
	return err
}

// ResolveBatch applies APPROVE/REJECT on the batch header without posting.
func (c *Client) ResolveBatch(ctx context.Context, batchID, batchType, decision, decidedBy, note string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveBatch(callCtx, &loanv1.ResolveBatchRequest{
		BatchId:   batchID,
		BatchType: batchType,
		Decision:  decision,
		DecidedBy: decidedBy,
		Note:      note,
	})
	return err
}

// CheckGeneralProvision validates the period is actionable (BPMN validate).
func (c *Client) CheckGeneralProvision(ctx context.Context, id string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckGeneralProvision(callCtx, &loanv1.CheckGeneralProvisionRequest{
		GeneralProvisionId: id,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// ResolveGeneralProvision APPROVE recomputes + posts the provision delta and
// marks the period POSTED; REJECT just closes it.
func (c *Client) ResolveGeneralProvision(ctx context.Context, id, decision, decidedBy, note string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveGeneralProvision(callCtx, &loanv1.ResolveGeneralProvisionRequest{
		GeneralProvisionId: id,
		Decision:           decision,
		DecidedBy:          decidedBy,
		Note:               note,
	})
	return err
}

// SpecificProvisioner is the narrow surface the workflow LNM.306 workers need.
type SpecificProvisioner interface {
	CheckSpecificProvision(ctx context.Context, id string) (bool, string, error)
	ResolveSpecificProvision(ctx context.Context, id, decision, decidedBy, note string) error
}

// CheckSpecificProvision validates the request is actionable (BPMN validate).
func (c *Client) CheckSpecificProvision(ctx context.Context, id string) (bool, string, error) {
	if c == nil {
		return false, "", errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.api.CheckSpecificProvision(callCtx, &loanv1.CheckSpecificProvisionRequest{
		SpecificProvisionId: id,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetOk(), resp.GetMessage(), nil
}

// ResolveSpecificProvision APPROVE recomputes + posts the LNM_PROVISION_306
// card; REJECT closes the request.
func (c *Client) ResolveSpecificProvision(ctx context.Context, id, decision, decidedBy, note string) error {
	if c == nil {
		return errors.New("loan client is nil")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.api.ResolveSpecificProvision(callCtx, &loanv1.ResolveSpecificProvisionRequest{
		SpecificProvisionId: id,
		Decision:            decision,
		DecidedBy:           decidedBy,
		Note:                note,
	})
	return err
}
