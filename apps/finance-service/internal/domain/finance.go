package domain

import "time"

// AccountType defines the type of account in the chart of accounts.
type AccountType string

const (
	AccountTypeAsset     AccountType = "ASSET"
	AccountTypeLiability AccountType = "LIABILITY"
	AccountTypeEquity    AccountType = "EQUITY"
	AccountTypeIncome    AccountType = "INCOME"
	AccountTypeExpense   AccountType = "EXPENSE"
)

// NormalBalance indicates whether debits or credits increase the balance.
type NormalBalance string

const (
	NormalDebit  NormalBalance = "DEBIT"
	NormalCredit NormalBalance = "CREDIT"
)

// Account represents a chart of accounts entry.
type Account struct {
	ID            string       `json:"id"`
	TenantID      string       `json:"tenantId"`
	Code          string       `json:"code"`
	Name          string       `json:"name"`
	Type          AccountType  `json:"type"`
	NormalBalance NormalBalance `json:"normalBalance"`
	Currency      string       `json:"currency"`
	IsActive      bool         `json:"isActive"`
	ParentID      string       `json:"parentId,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

// EntryType for double-entry ledger.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// TransactionStatus tracks lifecycle.
type TransactionStatus string

const (
	TxnPending   TransactionStatus = "PENDING"
	TxnPosted    TransactionStatus = "POSTED"
	TxnReversed TransactionStatus = "REVERSED"
	TxnFailed    TransactionStatus = "FAILED"
)

// Transaction is a financial transaction.
type Transaction struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenantId"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	TxnType        string            `json:"txnType"`
	TxnDate        string            `json:"txnDate"`
	PostedAt       time.Time         `json:"postedAt"`
	Status         TransactionStatus `json:"status"`
	Description    string            `json:"description,omitempty"`
	SourceRef      string            `json:"sourceRef,omitempty"`
	CreatedBy      string            `json:"createdBy"`
	ApprovedBy     string            `json:"approvedBy,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	Entries        []LedgerEntry     `json:"entries,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
}

// LedgerEntry is a single debit/credit line in a transaction.
type LedgerEntry struct {
	ID            string    `json:"id"`
	EntryID       string    `json:"entryId"`      // groups debit+credit for the same entry
	TransactionID string    `json:"transactionId"`
	AccountID     string    `json:"accountId"`
	AccountCode   string    `json:"accountCode,omitempty"`
	EntryType     EntryType `json:"entryType"`
	Amount        string    `json:"amount"`        // decimal string
	Currency      string    `json:"currency"`
	PostedAt      time.Time `json:"postedAt"`
	Description   string    `json:"description,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// AccountBalance is a materialized balance for an account.
type AccountBalance struct {
	AccountID string    `json:"accountId"`
	Balance   string    `json:"balance"`   // decimal string
	AsOf      time.Time `json:"asOf"`
}
