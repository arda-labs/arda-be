package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrUnknownTool         = errors.New("unknown AI tool")
	ErrToolForbidden       = errors.New("AI tool permission denied")
	ErrInvalidArgument     = errors.New("invalid AI tool arguments")
	ErrApprovalRequired    = errors.New("AI tool requires human approval")
	ErrApprovalUnavailable = errors.New("AI tool approval is not available")
	ErrApprovalPending     = errors.New("AI tool approval pending")
)

// ApprovalPending describes a human-approval proposal created in place of a
// tool execution. The agent loop turns it into an AG-UI interrupt; the run
// stays WAITING_APPROVAL until the proposal is decided and resumed.
type ApprovalPending struct {
	ProposalID string         `json:"proposalId"`
	Tool       string         `json:"tool"`
	Version    int            `json:"version"`
	Risk       string         `json:"risk"`
	Args       map[string]any `json:"args,omitempty"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}

// ApprovalPendingError lets a tool executor (the Code Mode sandbox closure)
// signal that a call was converted into a proposal without executing. The
// sandbox meta-tool unwraps it into Result.Approval instead of an error.
type ApprovalPendingError struct {
	Proposal ApprovalPending
}

func (e *ApprovalPendingError) Error() string {
	if e == nil {
		return ErrApprovalPending.Error()
	}
	return "approval pending for " + e.Proposal.Tool
}

func (e *ApprovalPendingError) Unwrap() error { return ErrApprovalPending }

type Definition struct {
	Name                string
	Version             int
	Kind                string
	Description         string
	RequiredPermissions []string
	Risk                string
	Timeout             time.Duration
	RedactionProfile    string
	Parameters          json.RawMessage
}

type Call struct {
	Name      string
	Version   int
	Arguments json.RawMessage
}

type Context struct {
	TenantID    string
	ActorUserID string
	OrgIDs      []string
	ActiveOrgID string
	RequestID   string
	TraceID     string
	Permissions map[string]struct{}

	// ExternalThread/ExternalRun are the AG-UI protocol ids of the durable run
	// currently executing. They are resolved server-side by the handler (from
	// the validated run input, or from the persisted run on resume) — never
	// from client headers or tool arguments. HITL proposal persistence needs
	// ExternalRun to attach the proposal to the owning ai_runs row.
	ExternalThread string
	ExternalRun    string

	// Identity context injected by the gateway (X-Username, X-User-Email,
	// X-Roles, X-Global-Roles, X-Global-Admin). Never trusted from the client
	// directly — the gateway strips and re-injects these headers.
	Username    string
	Email       string
	Roles       []string
	GlobalRoles []string
	GlobalAdmin bool
	AuthVersion string
}

type Result struct {
	Data      json.RawMessage
	Summary   string
	Source    string
	RequestID string
	FreshAt   time.Time
	// ErrorCode carries the machine-readable failure code when the tool
	// surfaced a structured error inside Data instead of returning it (the
	// Code Mode meta-tool does this so the model can inspect the failure).
	// The audit trail records this code and marks the execution FAILED;
	// callers that only look at (Result, error) must consult it too.
	ErrorCode string
	// Approval is set when the call produced a human-approval proposal instead
	// of executing. The agent loop must emit an interrupt and stop the run;
	// treating this as a completed tool call would strand the proposal.
	Approval *ApprovalPending
}

// SandboxError attaches the machine-readable code of a Code Mode sandbox
// failure (ai.sandbox_timeout, ai.sandbox_busy, ...) to the underlying error.
// The sandbox package produces it; the meta-tool reads it so audit rows and
// run metrics record the real outcome instead of SUCCEEDED.
type SandboxError struct {
	Code string
	Err  error
}

func (e *SandboxError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *SandboxError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ErrorCode resolves the machine-readable failure code of a tool execution
// error: the sandbox code when one is attached, the generic tool code
// otherwise.
func ErrorCode(err error) string {
	var sandboxErr *SandboxError
	if errors.As(err, &sandboxErr) && sandboxErr.Code != "" {
		return sandboxErr.Code
	}
	return "ai.tool_execution_failed"
}

type Tool interface {
	Definition() Definition
	Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error)
}

type Registry struct {
	items          map[string]Tool // Exposed to LLM in tool definitions
	executionItems map[string]Tool // Resolvable for resume/execution (includes HITL executors)
}

func NewRegistry(items ...Tool) *Registry {
	r := &Registry{
		items:          make(map[string]Tool, len(items)),
		executionItems: make(map[string]Tool, len(items)),
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		definition := item.Definition()
		r.items[definition.Name] = item
		r.executionItems[definition.Name] = item
	}
	return r
}

func (r *Registry) RegisterExecutionOnly(items ...Tool) {
	if r == nil {
		return
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		definition := item.Definition()
		r.executionItems[definition.Name] = item
	}
}

func (r *Registry) ResolveForExecution(call Call, scope Context) (Tool, Definition, error) {
	if r == nil {
		return nil, Definition{}, ErrUnknownTool
	}
	item, ok := r.executionItems[call.Name]
	if !ok {
		// Fallback to items
		item, ok = r.items[call.Name]
		if !ok {
			return nil, Definition{}, ErrUnknownTool
		}
	}
	definition := item.Definition()
	version := call.Version
	if version == 0 {
		version = 1
	}
	if version != definition.Version {
		return nil, Definition{}, ErrUnknownTool
	}
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.ActorUserID) == "" {
		return nil, Definition{}, ErrToolForbidden
	}
	for _, permission := range definition.RequiredPermissions {
		if _, allowed := scope.Permissions[permission]; !allowed {
			if _, superadmin := scope.Permissions["superadmin"]; !superadmin {
				return nil, Definition{}, ErrToolForbidden
			}
		}
	}
	return item, definition, nil
}

func (r *Registry) Definitions() []Definition {
	if r == nil {
		return nil
	}
	items := make([]Definition, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item.Definition())
	}
	return items
}

func (r *Registry) Resolve(call Call, scope Context) (Tool, Definition, error) {
	if r == nil {
		return nil, Definition{}, ErrUnknownTool
	}
	item, ok := r.items[call.Name]
	if !ok {
		return nil, Definition{}, ErrUnknownTool
	}
	definition := item.Definition()
	version := call.Version
	if version == 0 {
		version = 1
	}
	if version != definition.Version {
		return nil, Definition{}, ErrUnknownTool
	}
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.ActorUserID) == "" {
		return nil, Definition{}, ErrToolForbidden
	}
	for _, permission := range definition.RequiredPermissions {
		if _, allowed := scope.Permissions[permission]; !allowed {
			if _, superadmin := scope.Permissions["superadmin"]; !superadmin {
				return nil, Definition{}, ErrToolForbidden
			}
		}
	}
	if definition.Kind == "confirm" {
		return nil, definition, ErrApprovalRequired
	}
	return item, definition, nil
}
