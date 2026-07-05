package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type flexInt64 int64

func (v *flexInt64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*v = 0
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*v = 0
			return nil
		}
		parsed, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int64 string %q: %w", s, err)
		}
		*v = flexInt64(parsed)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	parsed, err := n.Int64()
	if err != nil {
		return fmt.Errorf("invalid int64 number %q: %w", n.String(), err)
	}
	*v = flexInt64(parsed)
	return nil
}

func (v flexInt64) Int64() int64 {
	return int64(v)
}
