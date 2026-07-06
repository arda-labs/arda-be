package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ZeebeUserTaskIndex reads native user tasks from Zeebe Elasticsearch exporter records.
// Required on Camunda 8.5 when Tasklist is disabled — gateway REST only supports assign/complete by key.
type ZeebeUserTaskIndex struct {
	baseURL    string
	httpClient *http.Client
}

func NewZeebeUserTaskIndex(baseURL string) *ZeebeUserTaskIndex {
	baseURL = normalizeHTTPBaseURL(baseURL)
	if baseURL == "" {
		return nil
	}
	return &ZeebeUserTaskIndex{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func DeriveZeebeESAddr(zeebeGRPC string) string {
	zeebeGRPC = strings.TrimSpace(zeebeGRPC)
	if zeebeGRPC == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(zeebeGRPC)
	if err != nil {
		return ""
	}
	switch {
	case strings.Contains(host, "zeebe-zeebe-gateway"):
		host = strings.Replace(host, "zeebe-zeebe-gateway", "zeebe-elasticsearch", 1)
	case strings.Contains(host, "zeebe-gateway"):
		host = strings.Replace(host, "zeebe-gateway", "zeebe-elasticsearch", 1)
	default:
		return ""
	}
	return "http://" + host + ":9200"
}

func (c *ZeebeUserTaskIndex) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *ZeebeUserTaskIndex) SearchUserTasks(ctx context.Context, processInstanceKey int64, state string) ([]ZeebeUserTask, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe elasticsearch index is not configured")
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}
	if state == "" {
		state = "CREATED"
	}

	body, err := json.Marshal(map[string]any{
		"size": 500,
		"sort": []map[string]string{{"position": "asc"}},
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []map[string]any{
					{"term": map[string]string{"valueType": "USER_TASK"}},
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
		return nil, fmt.Errorf("elasticsearch user task search: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("elasticsearch user task search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	tasks, err := activeUserTasksFromES(raw, state)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no user tasks in elasticsearch for process instance %d", processInstanceKey)
	}
	return tasks, nil
}

type esSearchResponse struct {
	Hits struct {
		Hits []struct {
			Source esUserTaskRecord `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type esUserTaskRecord struct {
	Intent    string          `json:"intent"`
	ValueType string          `json:"valueType"`
	Value     esUserTaskValue `json:"value"`
}

type esUserTaskValue struct {
	UserTaskKey        json.Number `json:"userTaskKey"`
	ElementID          string      `json:"elementId"`
	ProcessInstanceKey json.Number `json:"processInstanceKey"`
	CandidateGroups    []string    `json:"candidateGroups"`
	CandidateGroupsList []string   `json:"candidateGroupsList"`
	Assignee           string      `json:"assignee"`
}

func activeUserTasksFromES(raw []byte, wantState string) ([]ZeebeUserTask, error) {
	var parsed esSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode elasticsearch user task search: %w", err)
	}

	type tracked struct {
		intent string
		task   ZeebeUserTask
	}
	byKey := map[int64]tracked{}

	for _, hit := range parsed.Hits.Hits {
		rec := hit.Source
		if rec.ValueType != "" && rec.ValueType != "USER_TASK" {
			continue
		}
		task, err := rec.Value.toUserTask(rec.Intent)
		if err != nil {
			slog.Debug("skip malformed elasticsearch user task record", "err", err)
			continue
		}
		byKey[task.UserTaskKey] = tracked{intent: strings.ToUpper(strings.TrimSpace(rec.Intent)), task: task}
	}

	out := make([]ZeebeUserTask, 0)
	for _, item := range byKey {
		if !isActiveUserTaskIntent(item.intent) {
			continue
		}
		task := item.task
		task.State = wantState
		out = append(out, task)
	}
	return out, nil
}

func isActiveUserTaskIntent(intent string) bool {
	switch intent {
	case "CREATED", "ASSIGNED", "UPDATED", "ASSIGNING", "UPDATING":
		return true
	default:
		return false
	}
}

func (v esUserTaskValue) toUserTask(intent string) (ZeebeUserTask, error) {
	key, err := v.UserTaskKey.Int64()
	if err != nil {
		return ZeebeUserTask{}, fmt.Errorf("userTaskKey: %w", err)
	}
	pik, err := v.ProcessInstanceKey.Int64()
	if err != nil {
		return ZeebeUserTask{}, fmt.Errorf("processInstanceKey: %w", err)
	}
	groups := v.CandidateGroupsList
	if len(groups) == 0 {
		groups = v.CandidateGroups
	}
	return ZeebeUserTask{
		UserTaskKey:        key,
		ElementID:          strings.TrimSpace(v.ElementID),
		ProcessInstanceKey: pik,
		State:              strings.TrimSpace(intent),
		CandidateGroups:    groups,
		Assignee:           strings.TrimSpace(v.Assignee),
	}, nil
}

// ActiveUserTasksFromESForTest exposes ES record folding for unit tests.
func ActiveUserTasksFromESForTest(raw []byte, wantState string) ([]ZeebeUserTask, error) {
	return activeUserTasksFromES(raw, wantState)
}
