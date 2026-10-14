package extractor

import (
	"encoding/json"
	"errors"
	"strings"
)

var ErrNoJSONObject = errors.New("no JSON object in model output")

// ExtractJSONObject returns the first JSON object that decodes cleanly from
// any '{' in s. Models wrap JSON in code fences or prose; we do not ask for
// response_format because providers implement it inconsistently.
func ExtractJSONObject(s string) (map[string]any, error) {
	start := strings.IndexByte(s, '{')
	for start >= 0 {
		var obj map[string]any
		if err := json.NewDecoder(strings.NewReader(s[start:])).Decode(&obj); err == nil && obj != nil {
			return obj, nil
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return nil, ErrNoJSONObject
}
