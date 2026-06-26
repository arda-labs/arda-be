package domain

import "time"

// ApprovalStatus tracks the lifecycle of an approval request.
type ApprovalStatus string

const (
	ApprovalPending    ApprovalStatus = "PENDING"
	ApprovalPendingL2  ApprovalStatus = "PENDING_L2"
	ApprovalPendingL3  ApprovalStatus = "PENDING_L3"
	ApprovalApproved   ApprovalStatus = "APPROVED"
	ApprovalRejected   ApprovalStatus = "REJECTED"
	ApprovalCancelled  ApprovalStatus = "CANCELLED"
)

// ApprovalRequest represents a request for approval of a transaction.
type ApprovalRequest struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenantId"`
	RequestType  string         `json:"requestType"` // TRANSFER, DEPOSIT, WITHDRAWAL, ACCOUNT_OPENING
	RefID        string         `json:"refId"`       // ID của transaction
	Status       ApprovalStatus `json:"status"`
	CurrentLevel int            `json:"currentLevel"`
	TotalLevels  int            `json:"totalLevels"`
	MakerID      string         `json:"makerId"`
	MakerNote    string         `json:"makerNote,omitempty"`
	Amount       string         `json:"amount,omitempty"`
	Currency     string         `json:"currency"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    *time.Time     `json:"updatedAt,omitempty"`
	CompletedAt  *time.Time     `json:"completedAt,omitempty"`
	Steps        []ApprovalStep `json:"steps,omitempty"`
}

// ApprovalStep records each checker's decision.
type ApprovalStep struct {
	ID         string     `json:"id"`
	RequestID  string     `json:"requestId"`
	Level      int        `json:"level"`
	CheckerID  string     `json:"checkerId"`
	Decision   string     `json:"decision,omitempty"` // APPROVED, REJECTED
	Note       string     `json:"note,omitempty"`
	DecidedAt  *time.Time `json:"decidedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// ApprovalTier defines the approval levels for a given amount range.
type ApprovalTier struct {
	MaxAmount float64 // transaction amount upper bound
	Levels    int     // number of approval levels required
}

// ApprovalRule defines the approval policy for a transaction type.
type ApprovalRule struct {
	Type  string         `json:"type"`
	Tiers []ApprovalTier `json:"tiers"`
}
