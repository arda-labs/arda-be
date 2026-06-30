package policy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Route defines an authorization rule for a path/method.
type Route struct {
	ID          string   `yaml:"id"`
	Path        string   `yaml:"path"`
	Methods     []string `yaml:"methods,omitempty"`
	Auth        bool     `yaml:"auth"`
	Risk        string   `yaml:"risk,omitempty"`
	Permissions []string `yaml:"permissions,omitempty"`
}

// Policy is the top-level route policy configuration.
type Policy struct {
	Routes []Route `yaml:"routes"`
}

// Load reads a policy YAML file.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}

	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy file: %w", err)
	}

	for i := range p.Routes {
		if p.Routes[i].Risk == "" {
			p.Routes[i].Risk = defaultRisk(p.Routes[i].Auth)
		}
		if !validRisk(p.Routes[i].Risk) {
			return nil, fmt.Errorf("route %q has invalid risk %q", p.Routes[i].ID, p.Routes[i].Risk)
		}
		for j := range p.Routes[i].Methods {
			p.Routes[i].Methods[j] = strings.ToUpper(p.Routes[i].Methods[j])
		}
	}

	return &p, nil
}

// MatchResult is the outcome of matching a request against the policy.
type MatchResult struct {
	Route       *Route
	Public      bool
	RequireAuth bool
}

// Match finds the most specific route for the given path and method.
func (p *Policy) Match(path, method string) (*MatchResult, error) {
	method = strings.ToUpper(method)

	var best *Route
	for i := range p.Routes {
		r := &p.Routes[i]
		if !pathMatches(r.Path, path) {
			continue
		}
		if len(r.Methods) > 0 && !contains(r.Methods, method) {
			continue
		}
		if best == nil || specificity(r.Path) > specificity(best.Path) {
			best = r
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no route matched")
	}

	return &MatchResult{
		Route:       best,
		Public:      !best.Auth,
		RequireAuth: best.Auth,
	}, nil
}

func pathMatches(pattern, path string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	path = strings.TrimSuffix(path, "/")

	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}

	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}

	return pattern == path
}

func specificity(pattern string) int {
	if strings.HasSuffix(pattern, "/**") {
		return len(strings.TrimSuffix(pattern, "/**"))
	}
	if strings.HasSuffix(pattern, "/*") {
		return len(strings.TrimSuffix(pattern, "/*"))
	}
	return len(pattern) + 10
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func defaultRisk(auth bool) string {
	if !auth {
		return "public"
	}
	return "medium"
}

func validRisk(risk string) bool {
	switch risk {
	case "public", "low", "medium", "high":
		return true
	default:
		return false
	}
}
