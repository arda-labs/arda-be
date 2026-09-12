package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ZeebeIncident is an incident observed on the Zeebe Elasticsearch exporter.
type ZeebeIncident struct {
	IncidentKey          int64  `json:"incidentKey"`
	ProcessInstanceKey   int64  `json:"processInstanceKey"`
	ProcessDefinitionKey int64  `json:"processDefinitionKey,omitempty"`
	ElementInstanceKey   int64  `json:"elementInstanceKey,omitempty"`
	ElementID            string `json:"elementId,omitempty"`
	JobKey               int64  `json:"jobKey,omitempty"`
	ErrorType            string `json:"errorType,omitempty"`
	ErrorMessage         string `json:"errorMessage,omitempty"`
	State                string `json:"state"`
	BpmnProcessID        string `json:"bpmnProcessId,omitempty"`
	CreationTime         string `json:"creationTime,omitempty"`
}

// ZeebeIncidentIndex reads incident records from the Zeebe Elasticsearch
// exporter, the only incident source on Camunda 8.5 self-managed without
// Operate. Records are folded by incident key so the latest intent wins.
type ZeebeIncidentIndex struct {
	baseURL    string
	httpClient *http.Client
}

func NewZeebeIncidentIndex(baseURL string) *ZeebeIncidentIndex {
	baseURL = normalizeHTTPBaseURL(baseURL)
	if baseURL == "" {
		return nil
	}
	return &ZeebeIncidentIndex{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *ZeebeIncidentIndex) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// SearchIncidents returns incidents currently in CREATED state for a process
// instance. RESOLVED records only update the folding of older keys.
func (c *ZeebeIncidentIndex) SearchIncidents(ctx context.Context, processInstanceKey int64) ([]ZeebeIncident, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}

	body, err := json.Marshal(map[string]any{
		"size": 500,
		"sort": []map[string]string{{"position": "asc"}},
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []map[string]any{
					{"term": map[string]string{"valueType": "INCIDENT"}},
					{"term": map[string]string{"value.processInstanceKey": strconv.FormatInt(processInstanceKey, 10)}},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	url := c.baseURL + "/zeebe-record*/_search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch incident search: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("elasticsearch incident search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	incidents, err := incidentsFromES(raw)
	if err != nil {
		return nil, err
	}
	open := make([]ZeebeIncident, 0, len(incidents))
	for _, incident := range incidents {
		if incident.State == "CREATED" {
			open = append(open, incident)
		}
	}
	return open, nil
}

type esIncidentSearchResponse struct {
	Hits struct {
		Hits []struct {
			Source esIncidentRecord `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type esIncidentRecord struct {
	Position  json.Number     `json:"position"`
	Key       json.Number     `json:"key"`
	Timestamp esTime          `json:"timestamp"`
	Intent    string          `json:"intent"`
	ValueType string          `json:"valueType"`
	Value     esIncidentValue `json:"value"`
}

type esIncidentValue struct {
	IncidentKey          json.Number `json:"incidentKey"`
	ProcessInstanceKey   json.Number `json:"processInstanceKey"`
	ProcessDefinitionKey json.Number `json:"processDefinitionKey"`
	ElementInstanceKey   json.Number `json:"elementInstanceKey"`
	ElementID            string      `json:"elementId"`
	JobKey               json.Number `json:"jobKey"`
	ErrorType            string      `json:"errorType"`
	ErrorMessage         string      `json:"errorMessage"`
	BpmnProcessID        string      `json:"bpmnProcessId"`
	CreationTime         string      `json:"creationTime"`
}

// incidentsFromES folds records by incident key; the latest intent wins.
// On Zeebe 8.5 the incident key is the record key — the value object has no
// incidentKey field (older fixtures used value.incidentKey, kept as fallback).
func incidentsFromES(raw []byte) ([]ZeebeIncident, error) {
	var parsed esIncidentSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode elasticsearch incident search: %w", err)
	}

	byKey := map[int64]ZeebeIncident{}
	for _, hit := range parsed.Hits.Hits {
		rec := hit.Source
		if rec.ValueType != "" && rec.ValueType != "INCIDENT" {
			continue
		}
		incident, ok := incidentFromRecord(rec, jsonInt64(rec.Key))
		if !ok {
			continue
		}
		byKey[incident.IncidentKey] = mergeIncident(byKey[incident.IncidentKey], incident)
	}

	out := make([]ZeebeIncident, 0, len(byKey))
	for _, incident := range byKey {
		out = append(out, incident)
	}
	return out, nil
}

// incidentFromRecord builds the folded incident shape from one record. Fields
// may be missing on terminal (RESOLVED) records; callers fold in record order
// so a CREATED record usually provides them.
func incidentFromRecord(rec esIncidentRecord, recordKey int64) (ZeebeIncident, bool) {
	key := recordKey
	if v := jsonInt64(rec.Value.IncidentKey); v > 0 {
		key = v
	}
	if key <= 0 {
		return ZeebeIncident{}, false
	}
	created := rec.Timestamp.Time
	if strings.TrimSpace(rec.Value.CreationTime) != "" {
		var parsed esTime
		if parsed.UnmarshalJSON([]byte(rec.Value.CreationTime)) == nil && !parsed.Time.IsZero() {
			created = parsed.Time
		}
	}
	incident := ZeebeIncident{
		IncidentKey:        key,
		ProcessInstanceKey: jsonInt64(rec.Value.ProcessInstanceKey),
		ElementID:          strings.TrimSpace(rec.Value.ElementID),
		ErrorType:          strings.TrimSpace(rec.Value.ErrorType),
		ErrorMessage:       strings.TrimSpace(rec.Value.ErrorMessage),
		State:              strings.ToUpper(strings.TrimSpace(rec.Intent)),
		BpmnProcessID:      strings.TrimSpace(rec.Value.BpmnProcessID),
		CreationTime:       formatESTime(created),
	}
	incident.ProcessDefinitionKey = jsonInt64(rec.Value.ProcessDefinitionKey)
	incident.ElementInstanceKey = jsonInt64(rec.Value.ElementInstanceKey)
	incident.JobKey = jsonInt64(rec.Value.JobKey)
	return incident, true
}

// mergeIncident keeps fields from the CREATED record when a later terminal
// record arrives without them; the state always follows the newest record.
func mergeIncident(prev, next ZeebeIncident) ZeebeIncident {
	merged := prev
	if merged.IncidentKey == 0 {
		merged.IncidentKey = next.IncidentKey
	}
	merged.State = next.State
	if next.ProcessInstanceKey != 0 {
		merged.ProcessInstanceKey = next.ProcessInstanceKey
	}
	if next.ProcessDefinitionKey != 0 {
		merged.ProcessDefinitionKey = next.ProcessDefinitionKey
	}
	if next.ElementInstanceKey != 0 {
		merged.ElementInstanceKey = next.ElementInstanceKey
	}
	if next.ElementID != "" {
		merged.ElementID = next.ElementID
	}
	if next.JobKey != 0 {
		merged.JobKey = next.JobKey
	}
	if next.ErrorType != "" {
		merged.ErrorType = next.ErrorType
	}
	if next.ErrorMessage != "" {
		merged.ErrorMessage = next.ErrorMessage
	}
	if next.BpmnProcessID != "" {
		merged.BpmnProcessID = next.BpmnProcessID
	}
	if next.CreationTime != "" {
		merged.CreationTime = next.CreationTime
	}
	return merged
}

// OpenIncidentsFromESForTest exposes ES incident folding for unit tests.
func OpenIncidentsFromESForTest(raw []byte) []ZeebeIncident {
	incidents, err := incidentsFromES(raw)
	if err != nil {
		return []ZeebeIncident{}
	}
	open := make([]ZeebeIncident, 0, len(incidents))
	for _, incident := range incidents {
		if incident.State == "CREATED" {
			open = append(open, incident)
		}
	}
	return open
}
