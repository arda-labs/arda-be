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
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantId"`
	Code          string         `json:"code"`
	Name          string         `json:"name"`
	Type          AccountType    `json:"type"`
	NormalBalance NormalBalance  `json:"normalBalance"`
	Currency      string         `json:"currency"`
	IsActive      bool           `json:"isActive"`
	ParentID      string         `json:"parentId,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
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
	TxnDraft    TransactionStatus = "DRAFT"
	TxnPending  TransactionStatus = "PENDING"
	TxnPosted   TransactionStatus = "POSTED"
	TxnReversed TransactionStatus = "REVERSED"
	TxnFailed   TransactionStatus = "FAILED"
)

type TransactionDirection string

const (
	TxnDirectionIncoming TransactionDirection = "INCOMING"
	TxnDirectionOutgoing TransactionDirection = "OUTGOING"
)

// Transaction is a financial transaction.
type Transaction struct {
	ID                  string               `json:"id"`
	TenantID            string               `json:"tenantId"`
	IdempotencyKey      string               `json:"idempotencyKey,omitempty"`
	TxnType             string               `json:"txnType"`
	Direction           TransactionDirection `json:"direction,omitempty"`
	CaseType            string               `json:"caseType,omitempty"`
	OperationName       string               `json:"operationName,omitempty"`
	TxnDate             string               `json:"txnDate"`
	PostedAt            time.Time            `json:"postedAt"`
	Status              TransactionStatus    `json:"status"`
	Amount              string               `json:"amount,omitempty"`
	Currency            string               `json:"currency,omitempty"`
	Description         string               `json:"description,omitempty"`
	SourceRef           string               `json:"sourceRef,omitempty"`
	ReversedTransactionID string             `json:"reversedTransactionId,omitempty"`
	CounterpartyName    string               `json:"counterpartyName,omitempty"`
	CounterpartyAccount string               `json:"counterpartyAccount,omitempty"`
	CurrentStep         string               `json:"currentStep,omitempty"`
	Priority            string               `json:"priority,omitempty"`
	CreatedBy           string               `json:"createdBy"`
	ApprovedBy          string               `json:"approvedBy,omitempty"`
	Metadata            map[string]any       `json:"metadata,omitempty"`
	Entries             []LedgerEntry        `json:"entries,omitempty"`
	CreatedAt           time.Time            `json:"createdAt"`
}

// LedgerEntry is a single debit/credit line in a transaction.
type LedgerEntry struct {
	ID            string         `json:"id"`
	EntryID       string         `json:"entryId"` // groups debit+credit for the same entry
	TransactionID string         `json:"transactionId"`
	AccountID     string         `json:"accountId"`
	AccountCode   string         `json:"accountCode,omitempty"`
	EntryType     EntryType      `json:"entryType"`
	Amount        string         `json:"amount"` // decimal string
	Currency      string         `json:"currency"`
	PostedAt      time.Time      `json:"postedAt"`
	Description   string         `json:"description,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// AccountBalance is a materialized balance for an account.
type AccountBalance struct {
	AccountID string    `json:"accountId"`
	Balance   string    `json:"balance"` // decimal string
	AsOf      time.Time `json:"asOf"`
}

type ProcessConfig struct {
	ID                 string     `json:"id"`
	TenantID           string     `json:"tenantId"`
	CaseType           string     `json:"caseType"`
	BusinessArea       string     `json:"businessArea"`
	OperationName      string     `json:"operationName"`
	BPMNProcessID      string     `json:"bpmnProcessId"`
	BPMNVersion        int        `json:"bpmnVersion"`
	WorkflowEnabled    bool       `json:"workflowEnabled"`
	DefaultSLAPolicyID string     `json:"defaultSlaPolicyId,omitempty"`
	MakerRole          string     `json:"makerRole"`
	CheckerRole        string     `json:"checkerRole"`
	OwnerService       string     `json:"ownerService"`
	Status             string     `json:"status"`
	EffectiveFrom      time.Time  `json:"effectiveFrom"`
	EffectiveTo        *time.Time `json:"effectiveTo,omitempty"`
}

type AccountClassification struct {
	ID                    string `json:"id"`
	TenantID              string `json:"tenantId"`
	Code                  string `json:"code"`
	Name                  string `json:"name"`
	TxnType               string `json:"txnType"`
	Direction             string `json:"direction"`
	ProductCode           string `json:"productCode,omitempty"`
	Channel               string `json:"channel,omitempty"`
	OrgCode               string `json:"orgCode,omitempty"`
	AccountCode           string `json:"accountCode"`
	RegulatoryAccountCode string `json:"regulatoryAccountCode,omitempty"`
	InternalAccountCode   string `json:"internalAccountCode,omitempty"`
	Status                string `json:"status"`
}

type JournalLine struct {
	ID                      string `json:"id"`
	JournalDefinitionID     string `json:"journalDefinitionId"`
	LineSeq                 int    `json:"lineSeq"`
	EntryType               string `json:"entryType"`
	AccountResolutionType   string `json:"accountResolutionType"`
	AccountRef              string `json:"accountRef"`
	AmountSource            string `json:"amountSource"`
	DescriptionTemplate     string `json:"descriptionTemplate,omitempty"`
	Status                  string `json:"status"`
}

type JournalDefinition struct {
	ID                  string        `json:"id"`
	TenantID            string        `json:"tenantId"`
	Code                string        `json:"code"`
	Name                string        `json:"name"`
	TxnType             string        `json:"txnType"`
	Direction           string        `json:"direction"`
	AmountSource        string        `json:"amountSource,omitempty"`
	DescriptionTemplate string        `json:"descriptionTemplate,omitempty"`
	Status              string        `json:"status"`
	Lines               []JournalLine `json:"lines,omitempty"`
}

type NamedAccountMapping struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	AccountCode string `json:"accountCode"`
	Purpose     string `json:"purpose,omitempty"`
	Status      string `json:"status"`
}
