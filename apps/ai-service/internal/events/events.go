package events

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

const (
	SpecVersion = "1.0"
	EventSource = "arda://ai-service"

	StreamName = "AI_EVENTS"

	SubjectRunStarted   = "arda.ai.runs.started"
	SubjectRunFinished  = "arda.ai.runs.finished"
	SubjectRunFailed    = "arda.ai.runs.failed"
	SubjectRunCancelled = "arda.ai.runs.cancelled"

	SubjectApprovalRequested = "arda.ai.approvals.requested"
	SubjectApprovalDecided   = "arda.ai.approvals.decided"
	SubjectApprovalExecuted  = "arda.ai.approvals.executed"
	SubjectApprovalExpired   = "arda.ai.approvals.expired"

	SubjectKnowledgePublished = "arda.ai.knowledge.published"
	SubjectKnowledgeRetired   = "arda.ai.knowledge.retired"

	SubjectAuditToolDenied         = "arda.ai.audit.tool_denied"
	SubjectAuditSandboxRejected    = "arda.ai.audit.sandbox_rejected"
	SubjectAuditCrossTenantAttempt = "arda.ai.audit.cross_tenant_attempt"

	TypeRunStarted   = "ai.run.started"
	TypeRunFinished  = "ai.run.finished"
	TypeRunFailed    = "ai.run.failed"
	TypeRunCancelled = "ai.run.cancelled"

	TypeApprovalRequested = "ai.approval.requested"
	TypeApprovalDecided   = "ai.approval.decided"
	TypeApprovalExecuted  = "ai.approval.executed"
	TypeApprovalExpired   = "ai.approval.expired"

	TypeKnowledgePublished = "ai.knowledge.published"
	TypeKnowledgeRetired   = "ai.knowledge.retired"

	TypeAuditToolDenied         = "ai.audit.tool_denied"
	TypeAuditSandboxRejected    = "ai.audit.sandbox_rejected"
	TypeAuditCrossTenantAttempt = "ai.audit.cross_tenant_attempt"
)

type EventEnvelope struct {
	SpecVersion string `json:"specVersion"`
	ID          string `json:"id"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	Time        string `json:"time"`
	TenantID    string `json:"tenantId"`
	ActorUserID string `json:"actorUserId,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
	TraceID     string `json:"traceId,omitempty"`
	RunID       string `json:"runId,omitempty"`
	Data        any    `json:"data"`
}

type RunStartedData struct {
	ConversationID  string `json:"conversationId"`
	AgentID         string `json:"agentId"`
	Provider        string `json:"provider"`
	ModelID         string `json:"modelId"`
	ProtocolVersion string `json:"protocolVersion"`
	Mode            string `json:"mode"`
}

type RunFinishedData struct {
	ConversationID   string `json:"conversationId"`
	DurationMs       int64  `json:"durationMs"`
	InputTokens      int    `json:"inputTokens"`
	OutputTokens     int    `json:"outputTokens"`
	ToolCallCount    int    `json:"toolCallCount"`
	SandboxExecCount int    `json:"sandboxExecCount"`
	Mode             string `json:"mode"`
}

type RunFailedData struct {
	ConversationID string `json:"conversationId"`
	ErrorCode      string `json:"errorCode"`
	DurationMs     int64  `json:"durationMs"`
	Retryable      bool   `json:"retryable"`
}

type RunCancelledData struct {
	ConversationID string `json:"conversationId"`
	CancelledBy    string `json:"cancelledBy"`
	DurationMs     int64  `json:"durationMs"`
}

type ApprovalRequestedData struct {
	ApprovalID         string `json:"approvalId"`
	ToolName           string `json:"toolName"`
	SummaryRedacted    string `json:"summaryRedacted"`
	RequiredCapability string `json:"requiredCapability"`
	ExpiresAt          string `json:"expiresAt"`
}

type ApprovalDecidedData struct {
	ApprovalID     string `json:"approvalId"`
	Decision       string `json:"decision"`
	ApproverUserID string `json:"approverUserId"`
	SelfApproval   bool   `json:"selfApproval"`
}

type ApprovalExecutedData struct {
	ApprovalID     string `json:"approvalId"`
	ToolName       string `json:"toolName"`
	DurationMs     int64  `json:"durationMs"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type ApprovalExpiredData struct {
	ApprovalID string `json:"approvalId"`
	ToolName   string `json:"toolName"`
	ExpiredAt  string `json:"expiredAt"`
}

type KnowledgePublishedData struct {
	SourceID       string `json:"sourceId"`
	Version        int    `json:"version"`
	Scope          string `json:"scope"`
	Classification string `json:"classification"`
	ChunkCount     int    `json:"chunkCount"`
	Embedded       bool   `json:"embedded"`
}

type KnowledgeRetiredData struct {
	SourceID  string `json:"sourceId"`
	Version   int    `json:"version"`
	RetiredBy string `json:"retiredBy"`
}

type AuditToolDeniedData struct {
	ToolName          string `json:"toolName"`
	MissingPermission string `json:"missingPermission"`
	RiskLevel         string `json:"riskLevel"`
}

type AuditSandboxRejectedData struct {
	RejectionReason string `json:"rejectionReason"`
	ScriptHash      string `json:"scriptHash"`
}

type AuditCrossTenantAttemptData struct {
	AttemptedTenantID string `json:"attemptedTenantId"`
	ResolvedTenantID  string `json:"resolvedTenantId"`
	ToolName          string `json:"toolName"`
}

func NewEnvelope(
	eventType string,
	tenantID string,
	actorUserID string,
	requestID string,
	traceID string,
	runID string,
	data any,
) EventEnvelope {
	return EventEnvelope{
		SpecVersion: SpecVersion,
		ID:          generateEventID(),
		Type:        eventType,
		Source:      EventSource,
		Time:        time.Now().UTC().Format(time.RFC3339Nano),
		TenantID:    strings.TrimSpace(tenantID),
		ActorUserID: strings.TrimSpace(actorUserID),
		RequestID:   strings.TrimSpace(requestID),
		TraceID:     strings.TrimSpace(traceID),
		RunID:       strings.TrimSpace(runID),
		Data:        data,
	}
}

func generateEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
