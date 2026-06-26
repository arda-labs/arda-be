package policy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/casbin/casbin/v3/util"
)

// Enforcer wraps Casbin with custom ABAC functions and sync support.
type Enforcer struct {
	enforcer *casbin.Enforcer
}

// NewEnforcer creates a Casbin enforcer from a model file and an adapter.
func NewEnforcer(modelPath string, adapter persist.Adapter) (*Enforcer, error) {
	m, err := model.NewModelFromFile(modelPath)
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}

	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("new enforcer: %w", err)
	}

	// Register ABAC custom functions
	e.AddFunction("abacMatch", abacMatchFunc)
	e.AddFunction("ipInRange", ipInRangeFunc)
	e.AddFunction("isBusinessHours", isBusinessHoursFunc)

	// Load policies from adapter
	if err := e.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("load policy: %w", err)
	}

	slog.Info("casbin enforcer ready")

	return &Enforcer{enforcer: e}, nil
}

// Enforce checks if a subject can perform an action on an object with the given environment.
func (e *Enforcer) Enforce(sub, obj, act string, env map[string]any) (bool, error) {
	ok, err := e.enforcer.Enforce(sub, obj, act, env)
	if err != nil {
		return false, fmt.Errorf("enforce: %w", err)
	}
	return ok, nil
}

// AddPolicy adds a policy rule.
func (e *Enforcer) AddPolicy(sub, obj, act, eft string) error {
	_, err := e.enforcer.AddPolicy(sub, obj, act, eft)
	return err
}

// RemovePolicy removes a policy rule.
func (e *Enforcer) RemovePolicy(sub, obj, act, eft string) error {
	_, err := e.enforcer.RemovePolicy(sub, obj, act, eft)
	return err
}

// AddRoleForUser assigns a role to a user.
func (e *Enforcer) AddRoleForUser(user, role string) error {
	_, err := e.enforcer.AddGroupingPolicy(user, role)
	return err
}

// RemoveRoleForUser removes a role from a user.
func (e *Enforcer) RemoveRoleForUser(user, role string) error {
	_, err := e.enforcer.RemoveGroupingPolicy(user, role)
	return err
}

// GetRolesForUser returns roles assigned to a user.
func (e *Enforcer) GetRolesForUser(user string) ([]string, error) {
	return e.enforcer.GetRolesForUser(user)
}

// GetUsersForRole returns users that have a given role.
func (e *Enforcer) GetUsersForRole(role string) ([]string, error) {
	return e.enforcer.GetUsersForRole(role)
}

// LoadPolicy reloads policies from the adapter (call after watcher notification).
func (e *Enforcer) LoadPolicy() error {
	return e.enforcer.LoadPolicy()
}

// ── ABAC custom functions ──

func abacMatchFunc(args ...any) (any, error) {
	if len(args) < 2 {
		return false, nil
	}

	env, ok := args[0].(map[string]any)
	if !ok {
		return false, nil
	}
	sub, ok := args[1].(string)
	if !ok {
		return false, nil
	}

	// Check if user is enabled/disabled
	if status, ok := env["user_status"]; ok {
		if status != "ACTIVE" {
			return false, nil
		}
	}

	// Check ABAC on the subject's department/attributes
	_ = sub

	// Allow by default if no specific ABAC rules deny
	return true, nil
}

func ipInRangeFunc(args ...any) (any, error) {
	if len(args) < 2 {
		return false, nil
	}
	ip, ok1 := args[0].(string)
	cidr, ok2 := args[1].(string)
	if !ok1 || !ok2 {
		return false, nil
	}
	return util.IPMatch(ip, cidr), nil
}

func isBusinessHoursFunc(args ...any) (any, error) {
	if len(args) < 1 {
		return false, nil
	}
	env, ok := args[0].(map[string]any)
	if !ok {
		return false, nil
	}

	requireBusiness, _ := env["require_business_hours"].(bool)
	if !requireBusiness {
		return true, nil
	}

	now := time.Now()
	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false, nil
	}

	hour := now.Hour()
	return hour >= 8 && hour < 18, nil
}

// EnvBuilder helps construct environment maps for policy evaluation.
type EnvBuilder struct {
	env map[string]any
}

func NewEnv() *EnvBuilder {
	return &EnvBuilder{env: make(map[string]any)}
}

func (b *EnvBuilder) WithIP(ip string) *EnvBuilder {
	b.env["client_ip"] = ip
	return b
}

func (b *EnvBuilder) WithBusinessHours(req bool) *EnvBuilder {
	b.env["require_business_hours"] = req
	return b
}

func (b *EnvBuilder) WithStatus(status string) *EnvBuilder {
	b.env["user_status"] = status
	return b
}

func (b *EnvBuilder) With(key string, val any) *EnvBuilder {
	b.env[key] = val
	return b
}

func (b *EnvBuilder) Build() map[string]any {
	return b.env
}

// MarshalJSON serializes the env for debugging.
func (b *EnvBuilder) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.env)
}
