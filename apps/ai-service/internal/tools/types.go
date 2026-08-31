package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrUnknownTool      = errors.New("unknown AI tool")
	ErrToolForbidden    = errors.New("AI tool permission denied")
	ErrInvalidArgument  = errors.New("invalid AI tool arguments")
	ErrApprovalRequired = errors.New("AI tool requires human approval")
)

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
	Permissions map[string]struct{}

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
