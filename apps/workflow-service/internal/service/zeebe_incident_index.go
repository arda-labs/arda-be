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
	IncidentKey         int64  `json:"incidentKey"`
	ProcessInstanceKey  int64  `json:"processInstanceKey"`
	ProcessDefinitionKey int64 `json:"processDefinitionKey,omitempty"`
	ElementInstanceKey  int64  `json:"elementInstanceKey,omitempty"`
	ElementID           string `json:"elementId,omitempty"`
	JobKey              int64  `json:"jobKey,omitempty"`
	ErrorType           string `json:"errorType,omitempty"`
	ErrorMessage        string `json:"errorMessage,omitempty"`
	State               string `json:"state"`
	CreationTime        string `json:"creationTime,omitempty"`
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

	return openIncidentsFromES(raw), nil
}

type esIncidentSearchResponse struct {
	Hits struct {
		Hits []struct {
			Source esIncidentRecord `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type esIncidentRecord struct {
	Intent    string            `json:"intent"`
	ValueType string            `json:"valueType"`
	Value     esIncidentValue   `json:"value"`
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
	CreationTime         string      `json:"creationTime"`
}

func openIncidentsFromES(raw []byte) []ZeebeIncident {
	var parsed esIncidentSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return []ZeebeIncident{}
	}

	byKey := map[int64]ZeebeIncident{}
	for _, hit := range parsed.Hits.Hits {
		rec := hit.Source
		if rec.ValueType != "" && rec.ValueType != "INCIDENT" {
			continue
		}
		incident, err := rec.Value.toIncident(rec.Intent)
		if err != nil {
			continue
		}
		byKey[incident.IncidentKey] = incident
	}

	out := make([]ZeebeIncident, 0, len(byKey))
	for _, incident := range byKey {
		// A RESOLVED record replaces its CREATED record during folding, so
		// anything left here is an open incident.
		if incident.State != "CREATED" {
			continue
		}
		out = append(out, incident)
	}
	return out
}

func (v esIncidentValue) toIncident(intent string) (ZeebeIncident, error) {
	key, err := v.IncidentKey.Int64()
	if err != nil {
		return ZeebeIncident{}, fmt.Errorf("incidentKey: %w", err)
	}
	// processInstanceKey may be absent on terminal records (RESOLVED); the
	// query already filters by instance, so zero is acceptable for folding.
	pik, _ := v.ProcessInstanceKey.Int64()
	incident := ZeebeIncident{
		IncidentKey:        key,
		ProcessInstanceKey: pik,
		ElementID:          strings.TrimSpace(v.ElementID),
		ErrorType:          strings.TrimSpace(v.ErrorType),
		ErrorMessage:       strings.TrimSpace(v.ErrorMessage),
		State:              strings.ToUpper(strings.TrimSpace(intent)),
		CreationTime:       strings.TrimSpace(v.CreationTime),
	}
	if defKey, err := v.ProcessDefinitionKey.Int64(); err == nil {
		incident.ProcessDefinitionKey = defKey
	}
	if elemKey, err := v.ElementInstanceKey.Int64(); err == nil {
		incident.ElementInstanceKey = elemKey
	}
	if jobKey, err := v.JobKey.Int64(); err == nil {
		incident.JobKey = jobKey
	}
	return incident, nil
}

// OpenIncidentsFromESForTest exposes ES incident folding for unit tests.
func OpenIncidentsFromESForTest(raw []byte) []ZeebeIncident {
	return openIncidentsFromES(raw)
}
