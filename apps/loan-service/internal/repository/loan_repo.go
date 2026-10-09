package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// Sentinel errors mapped to HTTP statuses by the service layer.
var (
	ErrNotFound = errors.New("lnm: record not found")
	ErrConflict = errors.New("lnm: code conflict")
	// ErrContractNotEditable marks a maker-revise attempt against a contract
	// whose status left the DRAFT/PENDING_APPROVAL/REJECTED window between the service guard
	// read and the guarded UPDATE (race backstop).
	ErrContractNotEditable = errors.New("lnm: contract not editable")
	// ErrAdjustmentNotPending marks a workflow decision against an adjustment
	// that is not in PENDING anymore: either already resolved (target status
	// reached — the repository reports that as an idempotent no-op instead)
	// or still in a pre-submit state.
	ErrAdjustmentNotPending = domain.ErrAdjustmentNotPending
	// ErrCollectionNotApproved marks an attempt to post a receipt that is not
	// approved and has no journal entry proving an earlier successful settle.
	ErrCollectionNotApproved = errors.New("lnm: collection is not approved")
	ErrHeadroomExceeded      = errors.New("lnm: contract headroom exceeded")
	// ErrStaleVersion marks a guarded decision transition whose row version no
	// longer matches the one the checker saw: the dossier changed while it was
	// in review, so the decision must not be applied.
	ErrStaleVersion = errors.New("lnm: dossier changed while in review")
)

// NewID generates a prefixed random-hex identifier.
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure loan id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

type LoanRepository struct {
	db *sql.DB
}

func NewLoanRepository(db *sql.DB) *LoanRepository {
	return &LoanRepository{db: db}
}

// staleVersion reports whether a zero-row guarded transition was caused by a
// version mismatch (the dossier changed since the checker saw it) rather than
// by the row being absent or already terminal. The caller runs it only after
// its UPDATE (which carries the atomic `version = expected` predicate) missed.
// table is an internal constant, never user input.
func staleVersion(ctx context.Context, q repoTX, table, tenantID, id string, expected int64) bool {
	if expected <= 0 {
		return false
	}
	var current int64
	if err := q.QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current); err != nil {
		return false
	}
	return current != expected
}

// repoTX is the query surface shared by *sql.DB and *sql.Tx. Repository
// helpers take it so a caller can keep a state transition and its side
// effects in a single transaction (adjustment resolve) instead of the
// default "each helper opens its own tx" shape.
type repoTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return err
}

// orgCodesToAny: nil slice -> nil (unrestricted); slice -> []string for ANY().
func orgCodesToAny(orgCodes []string) any {
	if len(orgCodes) == 0 {
		return nil
	}
	return orgCodes
}
