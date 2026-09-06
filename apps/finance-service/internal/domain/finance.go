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
	ID                    string `json:"id"`
	JournalDefinitionID   string `json:"journalDefinitionId"`
	LineSeq               int    `json:"lineSeq"`
	EntryType             string `json:"entryType"`
	AccountResolutionType string `json:"accountResolutionType"`
	AccountRef            string `json:"accountRef"`
	AmountSource          string `json:"amountSource"`
	DescriptionTemplate   string `json:"descriptionTemplate,omitempty"`
	Status                string `json:"status"`
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
