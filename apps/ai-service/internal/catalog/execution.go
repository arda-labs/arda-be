package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// ExecutionResolver adapts the catalog dispatcher registry to the approval
// execution path (POST /api/ai/approvals/{id}/execution and AG-UI resume).
// Only confirm-kind entries are resolvable: an approval can never execute a
// read tool, and a proposal whose tool is no longer registered fails closed.
type ExecutionResolver struct {
	reg *DispatcherRegistry
}

func NewExecutionResolver(reg *DispatcherRegistry) *ExecutionResolver {
	return &ExecutionResolver{reg: reg}
}

func (r *ExecutionResolver) ResolveForExecution(call tools.Call, scope tools.Context) (tools.Tool, tools.Definition, error) {
	if r == nil || r.reg == nil {
		return nil, tools.Definition{}, tools.ErrUnknownTool
	}
	dispatch, entry, ok := r.reg.Resolve(call.Name)
	if !ok {
		return nil, tools.Definition{}, tools.ErrUnknownTool
	}
	if entry.Kind != "confirm" {
		return nil, tools.Definition{}, tools.ErrToolForbidden
	}
	version := call.Version
	if version == 0 {
		version = 1
	}
	if version != 1 {
		return nil, tools.Definition{}, tools.ErrUnknownTool
	}
	if err := entry.CheckPermissions(scope); err != nil {
		return nil, tools.Definition{}, err
	}
	tool := &dispatcherTool{entry: entry, dispatch: dispatch}
	return tool, tool.Definition(), nil
}

// dispatcherTool exposes a catalog dispatcher as a tools.Tool so the approval
// execution path can invoke it with the stored arguments after human approval.
type dispatcherTool struct {
	entry    CatalogEntry
	dispatch DispatcherFunc
}

func (t *dispatcherTool) Definition() tools.Definition {
	definition := tools.Definition{
		Name:                t.entry.MethodName,
		Version:             1,
		Kind:                t.entry.Kind,
		Description:         firstJSDocLine(t.entry.JSDoc),
		RequiredPermissions: t.entry.RequiredPermissions,
		Risk:                t.entry.Risk,
		Timeout:             t.entry.Timeout,
	}
	return definition
}

func (t *dispatcherTool) Execute(ctx context.Context, scope tools.Context, arguments json.RawMessage) (tools.Result, error) {
	args := make(map[string]any)
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return tools.Result{}, tools.ErrInvalidArgument
		}
	}
	out, err := t.dispatch(ctx, scope, args)
	if err != nil {
		return tools.Result{}, err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Data:      data,
		Summary:   "Đã thực thi " + t.entry.MethodName + ".",
		Source:    t.entry.Service,
		RequestID: scope.RequestID,
		FreshAt:   time.Now().UTC(),
	}, nil
}
