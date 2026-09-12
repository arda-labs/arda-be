package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ZeebeMonitoringIndex reads runtime state from the Zeebe Elasticsearch
// exporter. On Camunda 8.5 there is no Operate and the gateway REST API has no
// list/search endpoints (those arrive with the Orchestration Cluster API in
// 8.8+), so exporter records are the only global read model available.
//
// Records are folded by entity key and the latest intent wins, mirroring what
// Operate's importer would materialize. Every query filters by valueType and
// handlers additionally scope results to the caller tenant through
// business_cases (Zeebe itself runs with the <default> tenant id here).
type ZeebeMonitoringIndex struct {
	baseURL    string
	httpClient *http.Client
}

func NewZeebeMonitoringIndex(baseURL string) *ZeebeMonitoringIndex {
	baseURL = normalizeHTTPBaseURL(baseURL)
	if baseURL == "" {
		return nil
	}
	return &ZeebeMonitoringIndex{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *ZeebeMonitoringIndex) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// ZeebeProcessInstance is the folded state of one process instance.
type ZeebeProcessInstance struct {
	ProcessInstanceKey       string `json:"processInstanceKey"`
	ProcessDefinitionKey     string `json:"processDefinitionKey,omitempty"`
	BpmnProcessID            string `json:"bpmnProcessId"`
	Version                  int    `json:"version"`
	State                    string `json:"state"`
	StartTime                string `json:"startTime"`
	EndTime                  string `json:"endTime,omitempty"`
	ParentProcessInstanceKey string `json:"parentProcessInstanceKey,omitempty"`

	startAt time.Time
	endAt   time.Time
}

// ZeebeElementInstance is a folded flow-node activation of one instance.
type ZeebeElementInstance struct {
	ElementInstanceKey string `json:"elementInstanceKey"`
	ElementID          string `json:"elementId"`
	BpmnElementType    string `json:"bpmnElementType"`
	State              string `json:"state"`
	StartTime          string `json:"startTime"`
	EndTime            string `json:"endTime,omitempty"`
	FlowScopeKey       string `json:"flowScopeKey,omitempty"`
}

// ZeebeVariable is the latest value of one variable scope entry.
type ZeebeVariable struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	ScopeKey  string `json:"scopeKey"`
	UpdatedAt string `json:"updatedAt"`
}

// ZeebeJob is the folded state of one job.
type ZeebeJob struct {
	JobKey             string `json:"jobKey"`
	Type               string `json:"type"`
	State              string `json:"state"`
	Retries            int    `json:"retries"`
	Worker             string `json:"worker,omitempty"`
	ElementID          string `json:"elementId,omitempty"`
	ElementInstanceKey string `json:"elementInstanceKey,omitempty"`
	ProcessInstanceKey string `json:"processInstanceKey"`
	BpmnProcessID      string `json:"bpmnProcessId,omitempty"`
	ErrorMessage       string `json:"errorMessage,omitempty"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt,omitempty"`
}

// ProcessInstanceSearchParams filters the folded instance list. State and
// StartFrom/StartTo are applied after folding because they describe the
// current entity, not an individual record.
type ProcessInstanceSearchParams struct {
	State                    string
	BpmnProcessID            string
	ProcessDefinitionKey     int64
	ProcessInstanceKey       int64
	ParentProcessInstanceKey int64
	StartFrom                time.Time
	StartTo                  time.Time
	PageSize                 int
	Cursor                   int64
}

// IncidentSearchParams filters the folded incident list.
type IncidentSearchParams struct {
	State              string
	ErrorType          string
	BpmnProcessID      string
	ProcessInstanceKey int64
	From               time.Time
	To                 time.Time
	PageSize           int
	Cursor             int64
}

const monitoringRecordFetchLimit = 2000

var piRecordIncludes = []string{
	"position", "timestamp", "key", "intent", "valueType",
	"value.processInstanceKey", "value.processDefinitionKey", "value.bpmnProcessId",
	"value.version", "value.elementId", "value.bpmnElementType", "value.flowScopeKey",
	"value.parentProcessInstanceKey", "value.parentElementInstanceKey", "value.tenantId",
}

var incidentRecordIncludes = []string{
	"position", "timestamp", "key", "intent", "valueType",
	"value.incidentKey", "value.processInstanceKey", "value.processDefinitionKey",
	"value.elementId", "value.elementInstanceKey", "value.jobKey",
	"value.errorType", "value.errorMessage", "value.creationTime", "value.bpmnProcessId",
}

var variableRecordIncludes = []string{
	"position", "timestamp", "intent", "valueType",
	"value.name", "value.value", "value.scopeKey", "value.processInstanceKey",
}

var jobRecordIncludes = []string{
	"position", "timestamp", "key", "intent", "valueType",
	"value.type", "value.worker", "value.retries", "value.elementId",
	"value.elementInstanceKey", "value.processInstanceKey", "value.bpmnProcessId",
	"value.errorMessage", "value.errorCode",
}

// ─── Elasticsearch plumbing ─────────────────────────────────────────────────────

func (c *ZeebeMonitoringIndex) searchRaw(ctx context.Context, body map[string]any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/zeebe-record*/_search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch monitoring search: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("elasticsearch monitoring search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func esTerm(field string, value any) map[string]any {
	return map[string]any{"term": map[string]any{field: value}}
}

func esRange(field string, gte, lte any) map[string]any {
	bounds := map[string]any{}
	if gte != nil {
		bounds["gte"] = gte
	}
	if lte != nil {
		bounds["lte"] = lte
	}
	return map[string]any{"range": map[string]any{field: bounds}}
}

func topHitsAgg(order string, includes []string) map[string]any {
	return map[string]any{"top_hits": map[string]any{
		"size":    1,
		"sort":    []any{map[string]any{"position": map[string]any{"order": order}}},
		"_source": map[string]any{"includes": includes},
	}}
}

func filteredTopHits(filter map[string]any, order string, includes []string) map[string]any {
	return map[string]any{
		"filter": filter,
		"aggs":   map[string]any{"hit": topHitsAgg(order, includes)},
	}
}

type esTopHits struct {
	Hits struct {
		Hits []struct {
			Source json.RawMessage `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type esFilterAgg struct {
	Hit esTopHits `json:"hit"`
}

func (h esTopHits) firstSource() json.RawMessage {
	if len(h.Hits.Hits) == 0 {
		return nil
	}
	return h.Hits.Hits[0].Source
}

func decodeRaw(raw json.RawMessage, target any) bool {
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func jsonInt64(n json.Number) int64 {
	v, err := n.Int64()
	if err != nil {
		return 0
	}
	return v
}

func positiveKeyString(n json.Number) string {
	v := jsonInt64(n)
	if v <= 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

func formatESTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// esTime decodes both epoch-millis numbers (what the exporter actually writes)
// and RFC3339 strings.
type esTime struct{ time.Time }

func (t *esTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		t.Time = time.UnixMilli(ms).UTC()
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("invalid elasticsearch timestamp %q", s)
	}
	t.Time = parsed.UTC()
	return nil
}

// ─── Process instance records ───────────────────────────────────────────────────

type esProcessInstanceRecord struct {
	Position  json.Number `json:"position"`
	Key       json.Number `json:"key"`
	Timestamp esTime      `json:"timestamp"`
	Intent    string      `json:"intent"`
	ValueType string      `json:"valueType"`
	Value     struct {
		ProcessInstanceKey       json.Number `json:"processInstanceKey"`
		ProcessDefinitionKey     json.Number `json:"processDefinitionKey"`
		BpmnProcessID            string      `json:"bpmnProcessId"`
		Version                  int         `json:"version"`
		ElementID                string      `json:"elementId"`
		BpmnElementType          string      `json:"bpmnElementType"`
		FlowScopeKey             json.Number `json:"flowScopeKey"`
		ParentProcessInstanceKey json.Number `json:"parentProcessInstanceKey"`
		ParentElementInstanceKey json.Number `json:"parentElementInstanceKey"`
		TenantID                 string      `json:"tenantId"`
	} `json:"value"`
}

// processInstanceState maps root element intents onto Operate-style states.
func processInstanceState(intent string) string {
	switch strings.ToUpper(strings.TrimSpace(intent)) {
	case "ELEMENT_COMPLETING", "ELEMENT_COMPLETED":
		return "COMPLETED"
	case "ELEMENT_TERMINATING", "ELEMENT_TERMINATED":
		return "TERMINATED"
	default:
		return "ACTIVE"
	}
}

func elementInstanceState(intent string) string {
	switch strings.ToUpper(strings.TrimSpace(intent)) {
	case "ELEMENT_COMPLETING", "ELEMENT_COMPLETED":
		return "COMPLETED"
	case "ELEMENT_TERMINATING", "ELEMENT_TERMINATED":
		return "TERMINATED"
	case "ELEMENT_ACTIVATING", "ELEMENT_ACTIVATED":
		return "ACTIVE"
	default:
		return strings.ToUpper(strings.TrimSpace(intent))
	}
}

func foldProcessInstance(instanceKey int64, firstRaw, latestRaw json.RawMessage) (ZeebeProcessInstance, bool) {
	var first esProcessInstanceRecord
	if !decodeRaw(firstRaw, &first) {
		return ZeebeProcessInstance{}, false
	}
	var latest esProcessInstanceRecord
	if !decodeRaw(latestRaw, &latest) {
		latest = first
	}
	state := processInstanceState(latest.Intent)
	pi := ZeebeProcessInstance{
		ProcessInstanceKey:       strconv.FormatInt(instanceKey, 10),
		ProcessDefinitionKey:     positiveKeyString(first.Value.ProcessDefinitionKey),
		BpmnProcessID:            strings.TrimSpace(first.Value.BpmnProcessID),
		Version:                  first.Value.Version,
		State:                    state,
		StartTime:                formatESTime(first.Timestamp.Time),
		ParentProcessInstanceKey: positiveKeyString(first.Value.ParentProcessInstanceKey),
		startAt:                  first.Timestamp.Time,
	}
	if state != "ACTIVE" {
		pi.EndTime = formatESTime(latest.Timestamp.Time)
		pi.endAt = latest.Timestamp.Time
	}
	return pi, true
}

type esPICompositeResponse struct {
	Aggregations struct {
		Entities struct {
			AfterKey struct {
				EntityKey json.Number `json:"entityKey"`
			} `json:"after_key"`
			Buckets []struct {
				Key struct {
					EntityKey json.Number `json:"entityKey"`
				} `json:"key"`
				RootLatest esFilterAgg `json:"rootLatest"`
				RootFirst  esFilterAgg `json:"rootFirst"`
			} `json:"buckets"`
		} `json:"entities"`
	} `json:"aggregations"`
}

// SearchProcessInstances folds PROCESS_INSTANCE records by process instance
// key. Post-filters that describe the folded entity (state, start range) are
// applied in Go; the caller loops until a page is filled.
func (c *ZeebeMonitoringIndex) SearchProcessInstances(ctx context.Context, p ProcessInstanceSearchParams) ([]ZeebeProcessInstance, string, error) {
	if !c.Enabled() {
		return nil, "", fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	pageSize := clampPageSize(p.PageSize)
	bucketSize := clampBucketSize(pageSize)

	out := make([]ZeebeProcessInstance, 0, pageSize)
	cursor := ""
	after := p.Cursor
	for attempt := 0; attempt < 4; attempt++ {
		body := map[string]any{
			"size":             0,
			"track_total_hits": false,
			"query": map[string]any{"bool": map[string]any{
				"filter": processInstanceFilters(p),
			}},
			"aggs": map[string]any{
				"entities": map[string]any{
					"composite": map[string]any{
						"size": bucketSize,
						"sources": []any{map[string]any{
							"entityKey": map[string]any{"terms": map[string]any{
								"field": "value.processInstanceKey",
								"order": "desc",
							}},
						}},
						"after": map[string]any{"entityKey": after},
					},
					"aggs": map[string]any{
						"rootLatest": filteredTopHits(esTerm("value.bpmnElementType", "PROCESS"), "desc", piRecordIncludes),
						"rootFirst":  filteredTopHits(esTerm("value.bpmnElementType", "PROCESS"), "asc", piRecordIncludes),
					},
				},
			},
		}
		if after <= 0 {
			delete(body["aggs"].(map[string]any)["entities"].(map[string]any)["composite"].(map[string]any), "after")
		}

		raw, err := c.searchRaw(ctx, body)
		if err != nil {
			return nil, "", err
		}
		var parsed esPICompositeResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, "", fmt.Errorf("decode elasticsearch process instance search: %w", err)
		}

		buckets := parsed.Aggregations.Entities.Buckets
		for _, bucket := range buckets {
			pi, ok := foldProcessInstance(jsonInt64(bucket.Key.EntityKey), bucket.RootFirst.Hit.firstSource(), bucket.RootLatest.Hit.firstSource())
			if !ok {
				continue
			}
			if p.State != "" && !strings.EqualFold(pi.State, p.State) {
				continue
			}
			if !p.StartFrom.IsZero() && pi.startAt.Before(p.StartFrom) {
				continue
			}
			if !p.StartTo.IsZero() && !pi.startAt.IsZero() && pi.startAt.After(p.StartTo) {
				continue
			}
			out = append(out, pi)
			if len(out) >= pageSize {
				cursor = strconv.FormatInt(jsonInt64(bucket.Key.EntityKey), 10)
				return out, cursor, nil
			}
		}

		next := jsonInt64(parsed.Aggregations.Entities.AfterKey.EntityKey)
		if next == 0 || next == after {
			return out, "", nil
		}
		after = next
		if attempt == 3 {
			return out, strconv.FormatInt(after, 10), nil
		}
	}
	return out, cursor, nil
}

func processInstanceFilters(p ProcessInstanceSearchParams) []any {
	filters := []any{esTerm("valueType", "PROCESS_INSTANCE")}
	if p.BpmnProcessID != "" {
		filters = append(filters, esTerm("value.bpmnProcessId", p.BpmnProcessID))
	}
	if p.ProcessDefinitionKey > 0 {
		filters = append(filters, esTerm("value.processDefinitionKey", p.ProcessDefinitionKey))
	}
	if p.ProcessInstanceKey > 0 {
		filters = append(filters, esTerm("value.processInstanceKey", p.ProcessInstanceKey))
	}
	if p.ParentProcessInstanceKey > 0 {
		filters = append(filters, esTerm("value.parentProcessInstanceKey", p.ParentProcessInstanceKey))
	}
	return filters
}

// GetProcessInstance returns the folded state for one instance, or nil when the
// exporter has no records for it.
func (c *ZeebeMonitoringIndex) GetProcessInstance(ctx context.Context, processInstanceKey int64) (*ZeebeProcessInstance, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}
	raw, err := c.searchRaw(ctx, map[string]any{
		"size":             monitoringRecordFetchLimit,
		"track_total_hits": false,
		"sort":             []any{map[string]any{"position": map[string]any{"order": "asc"}}},
		"_source":          map[string]any{"includes": piRecordIncludes},
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			esTerm("valueType", "PROCESS_INSTANCE"),
			esTerm("value.processInstanceKey", processInstanceKey),
		}}},
	})
	if err != nil {
		return nil, err
	}
	records, err := decodeHits[esProcessInstanceRecord](raw)
	if err != nil {
		return nil, err
	}
	var first, latest *esProcessInstanceRecord
	for i := range records {
		if !strings.EqualFold(records[i].Value.BpmnElementType, "PROCESS") {
			continue
		}
		if first == nil {
			first = &records[i]
		}
		latest = &records[i]
	}
	if first == nil || latest == nil {
		return nil, nil
	}
	state := processInstanceState(latest.Intent)
	pi := &ZeebeProcessInstance{
		ProcessInstanceKey:       strconv.FormatInt(processInstanceKey, 10),
		ProcessDefinitionKey:     positiveKeyString(first.Value.ProcessDefinitionKey),
		BpmnProcessID:            strings.TrimSpace(first.Value.BpmnProcessID),
		Version:                  first.Value.Version,
		State:                    state,
		StartTime:                formatESTime(first.Timestamp.Time),
		ParentProcessInstanceKey: positiveKeyString(first.Value.ParentProcessInstanceKey),
		startAt:                  first.Timestamp.Time,
	}
	if state != "ACTIVE" {
		pi.EndTime = formatESTime(latest.Timestamp.Time)
		pi.endAt = latest.Timestamp.Time
	}
	return pi, nil
}

// ListElementInstances folds every flow-node activation of one instance. The
// record key is the element instance key (PROCESS_INSTANCE values carry no
// elementInstanceKey field on 8.5).
func (c *ZeebeMonitoringIndex) ListElementInstances(ctx context.Context, processInstanceKey int64) ([]ZeebeElementInstance, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}
	raw, err := c.searchRaw(ctx, map[string]any{
		"size":             monitoringRecordFetchLimit,
		"track_total_hits": false,
		"sort":             []any{map[string]any{"position": map[string]any{"order": "asc"}}},
		"_source":          map[string]any{"includes": piRecordIncludes},
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			esTerm("valueType", "PROCESS_INSTANCE"),
			esTerm("value.processInstanceKey", processInstanceKey),
		}}},
	})
	if err != nil {
		return nil, err
	}
	return elementInstancesFromES(raw), nil
}

type elementAcc struct {
	first     time.Time
	last      time.Time
	intent    string
	elementID string
	elemType  string
	scopeKey  string
}

func elementInstancesFromES(raw []byte) []ZeebeElementInstance {
	records, err := decodeHits[esProcessInstanceRecord](raw)
	if err != nil {
		return []ZeebeElementInstance{}
	}
	byKey := map[int64]*elementAcc{}
	for i := range records {
		rec := records[i]
		key := jsonInt64(rec.Key)
		if key <= 0 {
			continue
		}
		acc := byKey[key]
		if acc == nil {
			acc = &elementAcc{first: rec.Timestamp.Time}
			byKey[key] = acc
		}
		if !rec.Timestamp.Time.IsZero() {
			acc.last = rec.Timestamp.Time
		}
		acc.intent = rec.Intent
		if rec.Value.ElementID != "" {
			acc.elementID = strings.TrimSpace(rec.Value.ElementID)
		}
		if rec.Value.BpmnElementType != "" {
			acc.elemType = strings.TrimSpace(rec.Value.BpmnElementType)
		}
		if scope := positiveKeyString(rec.Value.FlowScopeKey); scope != "" {
			acc.scopeKey = scope
		}
	}

	out := make([]ZeebeElementInstance, 0, len(byKey))
	for key, acc := range byKey {
		item := ZeebeElementInstance{
			ElementInstanceKey: strconv.FormatInt(key, 10),
			ElementID:          acc.elementID,
			BpmnElementType:    acc.elemType,
			State:              elementInstanceState(acc.intent),
			StartTime:          formatESTime(acc.first),
			FlowScopeKey:       acc.scopeKey,
		}
		if item.State != "ACTIVE" {
			item.EndTime = formatESTime(acc.last)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartTime < out[j].StartTime
	})
	return out
}

// ─── Variable records ───────────────────────────────────────────────────────────

type esVariableRecord struct {
	Position  json.Number `json:"position"`
	Timestamp esTime      `json:"timestamp"`
	Intent    string      `json:"intent"`
	ValueType string      `json:"valueType"`
	Value     struct {
		Name               string      `json:"name"`
		Value              string      `json:"value"`
		ScopeKey           json.Number `json:"scopeKey"`
		ProcessInstanceKey json.Number `json:"processInstanceKey"`
	} `json:"value"`
}

// ListVariables returns the latest value per (scope, name) pair.
func (c *ZeebeMonitoringIndex) ListVariables(ctx context.Context, processInstanceKey int64) ([]ZeebeVariable, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}
	raw, err := c.searchRaw(ctx, map[string]any{
		"size":             monitoringRecordFetchLimit,
		"track_total_hits": false,
		"sort":             []any{map[string]any{"position": map[string]any{"order": "asc"}}},
		"_source":          map[string]any{"includes": variableRecordIncludes},
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			esTerm("valueType", "VARIABLE"),
			esTerm("value.processInstanceKey", processInstanceKey),
		}}},
	})
	if err != nil {
		return nil, err
	}
	return variablesFromES(raw), nil
}

func variablesFromES(raw []byte) []ZeebeVariable {
	records, err := decodeHits[esVariableRecord](raw)
	if err != nil {
		return []ZeebeVariable{}
	}
	type acc struct {
		value     string
		updatedAt time.Time
	}
	byKey := map[string]*acc{}
	for i := range records {
		rec := records[i]
		name := strings.TrimSpace(rec.Value.Name)
		if name == "" {
			continue
		}
		scope := jsonInt64(rec.Value.ScopeKey)
		mapKey := strconv.FormatInt(scope, 10) + "|" + name
		byKey[mapKey] = &acc{value: rec.Value.Value, updatedAt: rec.Timestamp.Time}
	}

	out := make([]ZeebeVariable, 0, len(byKey))
	for mapKey, item := range byKey {
		parts := strings.SplitN(mapKey, "|", 2)
		out = append(out, ZeebeVariable{
			Name:      parts[1],
			Value:     item.value,
			ScopeKey:  parts[0],
			UpdatedAt: formatESTime(item.updatedAt),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScopeKey == out[j].ScopeKey {
			return out[i].Name < out[j].Name
		}
		return out[i].ScopeKey < out[j].ScopeKey
	})
	return out
}

// ─── Job records ────────────────────────────────────────────────────────────────

type esJobRecord struct {
	Position  json.Number `json:"position"`
	Key       json.Number `json:"key"`
	Timestamp esTime      `json:"timestamp"`
	Intent    string      `json:"intent"`
	ValueType string      `json:"valueType"`
	Value     struct {
		Type               string      `json:"type"`
		Worker             string      `json:"worker"`
		Retries            json.Number `json:"retries"`
		ElementID          string      `json:"elementId"`
		ElementInstanceKey json.Number `json:"elementInstanceKey"`
		ProcessInstanceKey json.Number `json:"processInstanceKey"`
		BpmnProcessID      string      `json:"bpmnProcessId"`
		ErrorMessage       string      `json:"errorMessage"`
		ErrorCode          string      `json:"errorCode"`
	} `json:"value"`
}

func jobState(intent string, retries int) string {
	switch strings.ToUpper(strings.TrimSpace(intent)) {
	case "COMPLETED":
		return "COMPLETED"
	case "CANCELED":
		return "CANCELED"
	case "FAILED":
		return "FAILED"
	case "ERROR_THROWN":
		return "ERROR_THROWN"
	case "TIMED_OUT":
		return "TIMED_OUT"
	case "ACTIVATED":
		return "ACTIVATED"
	default:
		if retries > 0 {
			return "ACTIVATABLE"
		}
		return "FAILED"
	}
}

// ListJobs folds the job records of one process instance, latest intent wins.
func (c *ZeebeMonitoringIndex) ListJobs(ctx context.Context, processInstanceKey int64) ([]ZeebeJob, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}
	raw, err := c.searchRaw(ctx, map[string]any{
		"size":             monitoringRecordFetchLimit,
		"track_total_hits": false,
		"sort":             []any{map[string]any{"position": map[string]any{"order": "asc"}}},
		"_source":          map[string]any{"includes": jobRecordIncludes},
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			esTerm("valueType", "JOB"),
			esTerm("value.processInstanceKey", processInstanceKey),
		}}},
	})
	if err != nil {
		return nil, err
	}
	return jobsFromES(raw), nil
}

func jobsFromES(raw []byte) []ZeebeJob {
	records, err := decodeHits[esJobRecord](raw)
	if err != nil {
		return []ZeebeJob{}
	}
	type acc struct {
		record    esJobRecord
		createdAt time.Time
	}
	byKey := map[int64]*acc{}
	for i := range records {
		rec := records[i]
		key := jsonInt64(rec.Key)
		if key <= 0 {
			continue
		}
		entry := byKey[key]
		if entry == nil {
			entry = &acc{createdAt: rec.Timestamp.Time}
			byKey[key] = entry
		}
		if strings.TrimSpace(rec.Value.ErrorMessage) == "" && entry.record.Value.ErrorMessage != "" {
			rec.Value.ErrorMessage = entry.record.Value.ErrorMessage
		}
		entry.record = rec
	}

	out := make([]ZeebeJob, 0, len(byKey))
	for key, entry := range byKey {
		rec := entry.record
		retries := int(jsonInt64(rec.Value.Retries))
		out = append(out, ZeebeJob{
			JobKey:             strconv.FormatInt(key, 10),
			Type:               strings.TrimSpace(rec.Value.Type),
			State:              jobState(rec.Intent, retries),
			Retries:            retries,
			Worker:             strings.TrimSpace(rec.Value.Worker),
			ElementID:          strings.TrimSpace(rec.Value.ElementID),
			ElementInstanceKey: positiveKeyString(rec.Value.ElementInstanceKey),
			ProcessInstanceKey: positiveKeyString(rec.Value.ProcessInstanceKey),
			BpmnProcessID:      strings.TrimSpace(rec.Value.BpmnProcessID),
			ErrorMessage:       strings.TrimSpace(rec.Value.ErrorMessage),
			CreatedAt:          formatESTime(entry.createdAt),
			UpdatedAt:          formatESTime(rec.Timestamp.Time),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

// ─── Incidents ──────────────────────────────────────────────────────────────────

type esIncidentCompositeResponse struct {
	Aggregations struct {
		Entities struct {
			AfterKey struct {
				EntityKey json.Number `json:"entityKey"`
			} `json:"after_key"`
			Buckets []struct {
				Key struct {
					EntityKey json.Number `json:"entityKey"`
				} `json:"key"`
				Latest  esTopHits   `json:"latest"`
				Created esFilterAgg `json:"created"`
			} `json:"buckets"`
		} `json:"entities"`
	} `json:"aggregations"`
}

// SearchIncidents folds INCIDENT records by incident key. On 8.5 the incident
// key is the record key (the value object has no incidentKey field), and the
// incident index is created lazily on the first incident.
func (c *ZeebeMonitoringIndex) SearchIncidents(ctx context.Context, p IncidentSearchParams) ([]ZeebeIncident, string, error) {
	if !c.Enabled() {
		return nil, "", fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	pageSize := clampPageSize(p.PageSize)
	bucketSize := clampBucketSize(pageSize)

	filters := []any{esTerm("valueType", "INCIDENT")}
	if p.ErrorType != "" {
		filters = append(filters, esTerm("value.errorType", p.ErrorType))
	}
	if p.BpmnProcessID != "" {
		filters = append(filters, esTerm("value.bpmnProcessId", p.BpmnProcessID))
	}
	if p.ProcessInstanceKey > 0 {
		filters = append(filters, esTerm("value.processInstanceKey", p.ProcessInstanceKey))
	}
	if !p.From.IsZero() || !p.To.IsZero() {
		var gte, lte any
		if !p.From.IsZero() {
			gte = p.From.UTC().Format(time.RFC3339)
		}
		if !p.To.IsZero() {
			lte = p.To.UTC().Format(time.RFC3339)
		}
		filters = append(filters, esRange("timestamp", gte, lte))
	}

	out := make([]ZeebeIncident, 0, pageSize)
	cursor := ""
	after := p.Cursor
	for attempt := 0; attempt < 4; attempt++ {
		composite := map[string]any{
			"size": bucketSize,
			"sources": []any{map[string]any{
				"entityKey": map[string]any{"terms": map[string]any{
					"field": "key",
					"order": "desc",
				}},
			}},
		}
		if after > 0 {
			composite["after"] = map[string]any{"entityKey": after}
		}
		body := map[string]any{
			"size":             0,
			"track_total_hits": false,
			"query":            map[string]any{"bool": map[string]any{"filter": filters}},
			"aggs": map[string]any{
				"entities": map[string]any{
					"composite": composite,
					"aggs": map[string]any{
						"latest":  topHitsAgg("desc", incidentRecordIncludes),
						"created": filteredTopHits(esTerm("intent", "CREATED"), "asc", incidentRecordIncludes),
					},
				},
			},
		}

		raw, err := c.searchRaw(ctx, body)
		if err != nil {
			return nil, "", err
		}
		var parsed esIncidentCompositeResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, "", fmt.Errorf("decode elasticsearch incident search: %w", err)
		}

		for _, bucket := range parsed.Aggregations.Entities.Buckets {
			incidentKey := jsonInt64(bucket.Key.EntityKey)
			if incidentKey <= 0 {
				continue
			}
			source := bucket.Created.Hit.firstSource()
			if len(source) == 0 {
				source = bucket.Latest.firstSource()
			}
			var created esIncidentRecord
			if !decodeRaw(source, &created) {
				continue
			}
			incident, ok := incidentFromRecord(created, incidentKey)
			if !ok {
				continue
			}
			var latest esIncidentRecord
			if decodeRaw(bucket.Latest.firstSource(), &latest) {
				incident.State = strings.ToUpper(strings.TrimSpace(latest.Intent))
			}
			if p.State != "" && !strings.EqualFold(incident.State, p.State) {
				continue
			}
			out = append(out, incident)
			if len(out) >= pageSize {
				cursor = strconv.FormatInt(incidentKey, 10)
				return out, cursor, nil
			}
		}

		next := jsonInt64(parsed.Aggregations.Entities.AfterKey.EntityKey)
		if next == 0 || next == after {
			return out, "", nil
		}
		after = next
		if attempt == 3 {
			return out, strconv.FormatInt(after, 10), nil
		}
	}
	return out, cursor, nil
}

// GetIncident returns the folded incident for one incident key.
func (c *ZeebeMonitoringIndex) GetIncident(ctx context.Context, incidentKey int64) (*ZeebeIncident, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if incidentKey <= 0 {
		return nil, fmt.Errorf("incidentKey is required")
	}
	raw, err := c.searchRaw(ctx, map[string]any{
		"size":             50,
		"track_total_hits": false,
		"sort":             []any{map[string]any{"position": map[string]any{"order": "asc"}}},
		"_source":          map[string]any{"includes": incidentRecordIncludes},
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			esTerm("valueType", "INCIDENT"),
			esTerm("key", incidentKey),
		}}},
	})
	if err != nil {
		return nil, err
	}
	records, err := decodeHits[esIncidentRecord](raw)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	incident, ok := incidentFromRecord(records[0], incidentKey)
	if !ok {
		return nil, nil
	}
	incident.State = strings.ToUpper(strings.TrimSpace(records[len(records)-1].Intent))
	return &incident, nil
}

// OpenIncidentCounts returns the number of open incidents per process instance
// for the given page of instance keys — used for list badges.
func (c *ZeebeMonitoringIndex) OpenIncidentCounts(ctx context.Context, processInstanceKeys []int64) (map[int64]int, error) {
	counts := map[int64]int{}
	if !c.Enabled() || len(processInstanceKeys) == 0 {
		return counts, nil
	}
	values := make([]any, 0, len(processInstanceKeys))
	for _, key := range processInstanceKeys {
		if key > 0 {
			values = append(values, key)
		}
	}
	if len(values) == 0 {
		return counts, nil
	}
	raw, err := c.searchRaw(ctx, map[string]any{
		"size":             monitoringRecordFetchLimit,
		"track_total_hits": false,
		"sort":             []any{map[string]any{"position": map[string]any{"order": "asc"}}},
		"_source":          map[string]any{"includes": incidentRecordIncludes},
		"query": map[string]any{"bool": map[string]any{"filter": []any{
			esTerm("valueType", "INCIDENT"),
			map[string]any{"terms": map[string]any{"value.processInstanceKey": values}},
		}}},
	})
	if err != nil {
		return nil, err
	}
	incidents, err := incidentsFromES(raw)
	if err != nil {
		return nil, err
	}
	for _, incident := range incidents {
		if incident.State != "CREATED" || incident.ProcessInstanceKey <= 0 {
			continue
		}
		counts[incident.ProcessInstanceKey]++
	}
	return counts, nil
}

// ─── Hit decoding helpers ───────────────────────────────────────────────────────

func decodeHits[T any](raw []byte) ([]T, error) {
	var parsed struct {
		Hits struct {
			Hits []struct {
				Source T `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make([]T, 0, len(parsed.Hits.Hits))
	for _, hit := range parsed.Hits.Hits {
		out = append(out, hit.Source)
	}
	return out, nil
}

func clampPageSize(size int) int {
	if size <= 0 {
		return 25
	}
	if size > 100 {
		return 100
	}
	return size
}

func clampBucketSize(pageSize int) int {
	size := pageSize * 4
	if size < 200 {
		size = 200
	}
	if size > 500 {
		size = 500
	}
	return size
}

// ─── Exports for unit tests ─────────────────────────────────────────────────────

// ProcessInstanceStateForTest exposes intent-to-state folding.
func ProcessInstanceStateForTest(intent string) string {
	return processInstanceState(intent)
}

// FoldProcessInstanceForTest exposes instance folding for unit tests.
func FoldProcessInstanceForTest(instanceKey int64, firstRaw, latestRaw []byte) (ZeebeProcessInstance, bool) {
	return foldProcessInstance(instanceKey, firstRaw, latestRaw)
}

// ElementInstancesFromESForTest exposes element folding for unit tests.
func ElementInstancesFromESForTest(raw []byte) []ZeebeElementInstance {
	return elementInstancesFromES(raw)
}

// VariablesFromESForTest exposes variable folding for unit tests.
func VariablesFromESForTest(raw []byte) []ZeebeVariable {
	return variablesFromES(raw)
}

// JobsFromESForTest exposes job folding for unit tests.
func JobsFromESForTest(raw []byte) []ZeebeJob {
	return jobsFromES(raw)
}
