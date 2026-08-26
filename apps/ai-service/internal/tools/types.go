package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrUnknownTool       = errors.New("unknown AI tool")
	ErrToolForbidden     = errors.New("AI tool permission denied")
	ErrInvalidArgument   = errors.New("invalid AI tool arguments")
	ErrApprovalRequired  = errors.New("AI tool requires human approval")
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
	items map[string]Tool
}

func NewRegistry(items ...Tool) *Registry {
	registry := &Registry{items: make(map[string]Tool, len(items))}
	for _, item := range items {
		if item == nil {
			continue
		}
		definition := item.Definition()
		registry.items[definition.Name] = item
	}
	return registry
}

func (r *Registry) ResolveForExecution(call Call, scope Context) (Tool, Definition, error) {
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
