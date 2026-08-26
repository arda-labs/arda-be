package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	"github.com/dop251/goja"
)

const (
	DefaultExecutionTimeout = 3000 * time.Millisecond
	MaxSDKMethodCalls       = 50
	MaxConcurrentSandboxes  = 8
)

var (
	ErrSandboxTimeout = errors.New("ai.sandbox_timeout: script execution exceeded time limit")
	ErrBudgetExceeded = errors.New("ai.sandbox_budget_exceeded: maximum 50 SDK method calls exceeded")
	ErrSandboxBusy    = errors.New("ai.sandbox_busy: maximum concurrent sandbox executions reached")
)

type ExecutionResult struct {
	Output         any            `json:"output,omitempty"`
	Logs           []string       `json:"logs,omitempty"`
	DurationMs     int64          `json:"durationMs"`
	ScriptHash     string         `json:"scriptHash"`
	MethodsCalled  []string       `json:"methodsCalled"`
	Error          string         `json:"error,omitempty"`
	ApprovalNeeded bool           `json:"approvalNeeded,omitempty"`
	ProposalTool   string         `json:"proposalTool,omitempty"`
	ProposalArgs   map[string]any `json:"proposalArgs,omitempty"`
}

type SDKMethod struct {
	MethodName       string
	SDKPath          string
	Domain           string
	Timeout          time.Duration
	CheckPermissions func(scope tools.Context) error
	Dispatcher       func(ctx context.Context, scope tools.Context, args map[string]any) (any, error)
}

type MethodRegistry interface {
	AllSDKMethods() []SDKMethod
}

type Engine struct {
	registry MethodRegistry
	sem      chan struct{}
}

func NewEngine(registry MethodRegistry) *Engine {
	return &Engine{
		registry: registry,
		sem:      make(chan struct{}, MaxConcurrentSandboxes),
	}
}

// Execute runs the provided JavaScript in an isolated Goja VM with arda.* bindings.
func (e *Engine) Execute(ctx context.Context, scope tools.Context, code string) (ExecutionResult, error) {
	// 1. Static validation
	if err := ValidateScript(code); err != nil {
		return ExecutionResult{
			Error: err.Error(),
		}, err
	}

	// 2. Concurrency gate
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		return ExecutionResult{
			Error: ErrSandboxBusy.Error(),
		}, ErrSandboxBusy
	}

	start := time.Now()
	hashBytes := sha256.Sum256([]byte(code))
	scriptHash := hex.EncodeToString(hashBytes[:])

	res := ExecutionResult{
		ScriptHash:    scriptHash,
		MethodsCalled: make([]string, 0),
	}

	// 3. Setup Goja VM
	vm := goja.New()

	// Strip dangerous globals
	dangerousGlobals := []string{
		"eval", "Function", "Reflect", "Proxy", "process",
		"global", "globalThis", "require", "module", "exports",
		"setTimeout", "setInterval", "clearTimeout", "clearInterval",
		"XMLHttpRequest", "WebSocket", "fetch",
	}
	for _, g := range dangerousGlobals {
		_ = vm.GlobalObject().Delete(g)
	}

	// Strip Date.now / performance.now to prevent timing attacks
	if dateVal := vm.Get("Date"); dateVal != nil {
		if dateObj, ok := dateVal.(*goja.Object); ok {
			_ = dateObj.Delete("now")
		}
	}

	// Safe console.log buffer (max 4 KiB total logs)
	var consoleLogs []string
	var totalLogBytes int
	consoleObj := vm.NewObject()
	logFn := func(call goja.FunctionCall) goja.Value {
		var parts []string
		for _, arg := range call.Arguments {
			parts = append(parts, fmt.Sprintf("%v", arg.Export()))
		}
		line := strings.Join(parts, " ")
		if totalLogBytes < 4*1024 {
			consoleLogs = append(consoleLogs, line)
			totalLogBytes += len(line)
		}
		return goja.Undefined()
	}
	_ = consoleObj.Set("log", logFn)
	_ = consoleObj.Set("info", logFn)
	_ = consoleObj.Set("warn", logFn)
	_ = consoleObj.Set("error", logFn)
	_ = vm.Set("console", consoleObj)

	// Setup Execution Context & State
	var mu sync.Mutex
	callCount := 0
	var approvalRequiredErr error
	var approvalTool string
	var approvalArgs map[string]any

	// 4. Inject arda.* SDK Tree
	ardaObj := vm.NewObject()

	methods := e.registry.AllSDKMethods()
	for _, method := range methods {
		methodCopy := method
		jsHandler := func(call goja.FunctionCall) goja.Value {
			mu.Lock()
			callCount++
			if callCount > MaxSDKMethodCalls {
				mu.Unlock()
				panic(vm.ToValue(map[string]any{
					"code":    "budget_exceeded",
					"message": "Maximum SDK call budget of 50 calls exceeded",
				}))
			}
			res.MethodsCalled = append(res.MethodsCalled, methodCopy.SDKPath)
			mu.Unlock()

			// Check permissions in Go
			if methodCopy.CheckPermissions != nil {
				if err := methodCopy.CheckPermissions(scope); err != nil {
					panic(vm.ToValue(map[string]any{
						"code":    "permission_denied",
						"domain":  methodCopy.Domain,
						"method":  methodCopy.SDKPath,
						"message": err.Error(),
					}))
				}
			}

			// Parse arguments
			var rawArgs map[string]any
			if len(call.Arguments) > 0 {
				argVal := call.Arguments[0].Export()
				if m, ok := argVal.(map[string]any); ok {
					rawArgs = m
				}
			}
			if rawArgs == nil {
				rawArgs = make(map[string]any)
			}

			// Execute dispatcher
			timeout := methodCopy.Timeout
			if timeout <= 0 {
				timeout = DefaultExecutionTimeout
			}
			execCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			data, err := methodCopy.Dispatcher(execCtx, scope, rawArgs)
			if err != nil {
				if errors.Is(err, tools.ErrApprovalRequired) {
					mu.Lock()
					approvalRequiredErr = err
					approvalTool = methodCopy.MethodName
					approvalArgs = rawArgs
					mu.Unlock()
					panic(vm.ToValue(map[string]any{
						"code":    "approval_required",
						"domain":  methodCopy.Domain,
						"method":  methodCopy.SDKPath,
						"message": "Action requires human approval",
					}))
				}
				panic(vm.ToValue(map[string]any{
					"code":    "execution_failed",
					"domain":  methodCopy.Domain,
					"method":  methodCopy.SDKPath,
					"message": err.Error(),
				}))
			}

			return vm.ToValue(data)
		}

		setNestedMethod(vm, ardaObj, method.SDKPath, jsHandler)
	}

	_ = vm.Set("arda", ardaObj)

	// 5. Setup Interrupt Timeout
	timer := time.AfterFunc(DefaultExecutionTimeout, func() {
		vm.Interrupt(ErrSandboxTimeout.Error())
	})
	defer timer.Stop()

	// 6. Wrap script in strict mode and async runner
	wrappedScript := fmt.Sprintf(`"use strict";
(function() {
	var __script_fn = async function() {
%s
	};
	return __script_fn();
})();`, indentCode(code))

	val, err := vm.RunString(wrappedScript)
	res.DurationMs = time.Since(start).Milliseconds()
	if len(consoleLogs) > 0 {
		res.Logs = consoleLogs
	}

	if err != nil {
		if approvalRequiredErr != nil {
			res.ApprovalNeeded = true
			res.ProposalTool = approvalTool
			res.ProposalArgs = approvalArgs
			return res, nil
		}

		errMsg := err.Error()
		if strings.Contains(errMsg, ErrSandboxTimeout.Error()) {
			res.Error = ErrSandboxTimeout.Error()
			return res, ErrSandboxTimeout
		}
		res.Error = errMsg
		return res, err
	}

	// If the promise returned, export value
	if val != nil {
		exported := val.Export()
		if p, ok := exported.(*goja.Promise); ok {
			switch p.State() {
			case goja.PromiseStateFulfilled:
				res.Output = p.Result().Export()
			case goja.PromiseStateRejected:
				rejErr := p.Result().Export()
				if approvalRequiredErr != nil {
					res.ApprovalNeeded = true
					res.ProposalTool = approvalTool
					res.ProposalArgs = approvalArgs
					return res, nil
				}
				res.Error = fmt.Sprintf("Promise rejected: %v", rejErr)
				return res, fmt.Errorf("script promise rejected: %v", rejErr)
			default:
				res.Output = exported
			}
		} else {
			res.Output = exported
		}
	}

	// Verify output size bounds
	if res.Output != nil {
		if b, err := json.Marshal(res.Output); err == nil {
			if len(b) > MaxOutputSizeBytes {
				res.Error = "ai.sandbox_output_too_large: result exceeded 64 KiB limit"
				res.Output = nil
				return res, errors.New(res.Error)
			}
		}
	}

	return res, nil
}

func setNestedMethod(vm *goja.Runtime, root *goja.Object, sdkPath string, fn func(goja.FunctionCall) goja.Value) {
	parts := strings.Split(sdkPath, ".")
	if len(parts) < 2 {
		return
	}
	if parts[0] == "arda" {
		parts = parts[1:]
	}

	current := root
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		existing := current.Get(part)
		if existing == nil || goja.IsUndefined(existing) || goja.IsNull(existing) {
			next := vm.NewObject()
			_ = current.Set(part, next)
			current = next
		} else if obj, ok := existing.(*goja.Object); ok {
			current = obj
		}
	}

	lastPart := parts[len(parts)-1]
	_ = current.Set(lastPart, fn)
}

func indentCode(code string) string {
	lines := strings.Split(code, "\n")
	for i, l := range lines {
		lines[i] = "\t\t" + l
	}
	return strings.Join(lines, "\n")
}
