package ardahttp

import (
	"encoding/json"
	"net/http"
	"time"
)

// ResponseMeta is optional correlation metadata echoed in JSON bodies.
type ResponseMeta struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id,omitempty"`
	Timestamp string `json:"timestamp"`
}

// NewResponseMeta builds meta from the incoming request and current UTC time.
func NewResponseMeta(r *http.Request) ResponseMeta {
	meta := ResponseMeta{
		RequestID: RequestID(r),
		Timestamp: formatResponseTimestamp(time.Now().UTC()),
	}
	if traceID := TraceID(r); traceID != "" {
		meta.TraceID = traceID
	}
	return meta
}

func formatResponseTimestamp(t time.Time) string {
	return t.Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z07:00")
}

func withResponseMeta(r *http.Request, data any) any {
	meta := NewResponseMeta(r)
	if data == nil {
		return map[string]any{"meta": meta}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return map[string]any{"meta": meta, "data": data}
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil && obj != nil {
		out := make(map[string]any, len(obj)+1)
		out["meta"] = meta
		for k, v := range obj {
			var val any
			_ = json.Unmarshal(v, &val)
			out[k] = val
		}
		return out
	}

	var val any
	_ = json.Unmarshal(raw, &val)
	return map[string]any{"meta": meta, "data": val}
}
