package ardahttp

import (
	"fmt"
	"net/url"
	"strings"
)

// QueryValueError describes a query parameter that cannot be parsed according
// to the endpoint contract.
type QueryValueError struct {
	Key      string
	Value    string
	Expected string
}

func (e *QueryValueError) Error() string {
	return fmt.Sprintf("invalid query parameter %s=%q; expected %s", e.Key, e.Value, e.Expected)
}

// ParseOptionalBool parses an optional strict true/false query parameter.
func ParseOptionalBool(values url.Values, key string) (*bool, error) {
	raw := strings.TrimSpace(values.Get(key))
	switch raw {
	case "":
		return nil, nil
	case "true":
		value := true
		return &value, nil
	case "false":
		value := false
		return &value, nil
	default:
		return nil, &QueryValueError{Key: key, Value: raw, Expected: "true or false"}
	}
}

// ParseCSVQuery parses a comma-separated filter, trims values and removes
// duplicates while preserving input order.
func ParseCSVQuery(values url.Values, key string) []string {
	seen := make(map[string]struct{})
	items := make([]string, 0)
	for _, raw := range strings.Split(values.Get(key), ",") {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}

// ParseOptionalEnum parses one optional query value against an allowlist.
func ParseOptionalEnum(values url.Values, key string, allowed ...string) (string, error) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return "", nil
	}
	for _, value := range allowed {
		if raw == value {
			return raw, nil
		}
	}
	return "", &QueryValueError{
		Key:      key,
		Value:    raw,
		Expected: strings.Join(allowed, ", "),
	}
}
