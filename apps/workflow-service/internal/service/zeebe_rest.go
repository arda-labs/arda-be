package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ZeebeUserTask is a native Zeebe user task (Camunda 8.5+), managed via the gateway REST API.
type ZeebeUserTask struct {
	UserTaskKey        int64
	ElementID          string
	ProcessInstanceKey int64
	State              string
	CandidateGroups    []string
	Assignee           string
}

// Tasklist search API (Camunda 8.5): POST /v1/tasks/search — flat body, not nested filter/page.
type tasklistTaskSearchRequest struct {
	State              string `json:"state,omitempty"`
	ProcessInstanceKey string `json:"processInstanceKey,omitempty"`
	TaskDefinitionID   string `json:"taskDefinitionId,omitempty"`
	PageSize           int    `json:"pageSize,omitempty"`
}

type tasklistTaskSearchItem struct {
	ID                 string   `json:"id"`
	TaskDefinitionID   string   `json:"taskDefinitionId"`
	ProcessInstanceKey string   `json:"processInstanceKey"`
	TaskState          string   `json:"taskState"`
	CandidateGroups    []string `json:"candidateGroups"`
	Assignee           string   `json:"assignee"`
}

// Orchestration / Zeebe gateway search (Camunda 8.6+ alpha): POST /v1/user-tasks/search.
type zeebeUserTaskSearchRequest struct {
	Filter struct {
		ProcessInstanceKey string `json:"processInstanceKey,omitempty"`
		State              string `json:"state,omitempty"`
		ElementID          string `json:"elementId,omitempty"`
	} `json:"filter,omitempty"`
	Page struct {
		From  int `json:"from"`
		Limit int `json:"limit"`
	} `json:"page"`
}

type zeebeUserTaskSearchResponse struct {
	Items []zeebeUserTaskItem `json:"items"`
}

type zeebeUserTaskItem struct {
	UserTaskKey        json.Number `json:"userTaskKey"`
	ElementID          string      `json:"elementId"`
	ProcessInstanceKey json.Number `json:"processInstanceKey"`
	State              string      `json:"state"`
	CandidateGroups    []string    `json:"candidateGroups"`
	Assignee           string      `json:"assignee"`
}

type ZeebeRestClient struct {
	baseURL        string
	tasklistBaseURL string
	httpClient     *http.Client
}

func NewZeebeRestClient(restAddr, tasklistAddr string) *ZeebeRestClient {
	restAddr = normalizeHTTPBaseURL(restAddr)
	if restAddr == "" {
		return nil
	}
	tasklistAddr = normalizeHTTPBaseURL(tasklistAddr)
	if tasklistAddr == "" {
		tasklistAddr = DeriveZeebeTasklistAddr(restAddr)
	}
	return &ZeebeRestClient{
		baseURL:         restAddr,
		tasklistBaseURL: tasklistAddr,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func normalizeHTTPBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/")
}

func DeriveZeebeRestAddr(grpcAddr string) string {
	grpcAddr = strings.TrimSpace(grpcAddr)
	if grpcAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(grpcAddr)
	if err != nil {
		return "http://" + grpcAddr + ":8080"
	}
	return "http://" + host + ":8080"
}

// DeriveZeebeTasklistAddr guesses the Tasklist service URL from the Zeebe gateway REST URL.
func DeriveZeebeTasklistAddr(restAddr string) string {
	restAddr = normalizeHTTPBaseURL(restAddr)
	if restAddr == "" {
		return ""
	}
	if strings.Contains(restAddr, "zeebe-gateway") {
		return strings.Replace(restAddr, "zeebe-gateway", "tasklist", 1)
	}
	if strings.Contains(restAddr, "zeebe-zeebe-gateway") {
		return strings.Replace(restAddr, "zeebe-zeebe-gateway", "zeebe-tasklist", 1)
	}
	return restAddr
}

func (c *ZeebeRestClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *ZeebeRestClient) SearchUserTasks(ctx context.Context, processInstanceKey int64, state string) ([]ZeebeUserTask, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("zeebe REST client is not configured")
	}
	if state == "" {
		state = "CREATED"
	}
	if processInstanceKey <= 0 {
		return nil, fmt.Errorf("processInstanceKey is required")
	}

	tasks, err := c.searchUserTasksViaTasklist(ctx, processInstanceKey, state)
	if err == nil {
		return tasks, nil
	}
	if !isHTTPNotFound(err) {
		slog.Debug("tasklist user task search failed, trying gateway search", "err", err)
	}

	gatewayTasks, gatewayErr := c.searchUserTasksViaGateway(ctx, processInstanceKey, state)
	if gatewayErr == nil {
		return gatewayTasks, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tasklist search: %w; gateway search: %v", err, gatewayErr)
	}
	return nil, gatewayErr
}

func (c *ZeebeRestClient) searchUserTasksViaTasklist(ctx context.Context, processInstanceKey int64, state string) ([]ZeebeUserTask, error) {
	if c.tasklistBaseURL == "" {
		return nil, fmt.Errorf("tasklist base URL is not configured")
	}
	reqBody := tasklistTaskSearchRequest{
		State:              state,
		ProcessInstanceKey: strconv.FormatInt(processInstanceKey, 10),
		PageSize:           50,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tasklistBaseURL+"/v1/tasks/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tasklist search user tasks: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, &httpStatusError{status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tasklist search user tasks HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	items, err := decodeTasklistSearchResponse(raw)
	if err != nil {
		return nil, err
	}
	out := make([]ZeebeUserTask, 0, len(items))
	for _, item := range items {
		ut, err := item.toUserTask(state)
		if err != nil {
			slog.Warn("skip malformed tasklist task item", "err", err)
			continue
		}
		out = append(out, ut)
	}
	return out, nil
}

func decodeTasklistSearchResponse(raw []byte) ([]tasklistTaskSearchItem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var items []tasklistTaskSearchItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}
	var wrapped struct {
		Tasks []tasklistTaskSearchItem `json:"tasks"`
		Items []tasklistTaskSearchItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("decode tasklist search: %w", err)
	}
	if len(wrapped.Tasks) > 0 {
		return wrapped.Tasks, nil
	}
	return wrapped.Items, nil
}

func (i tasklistTaskSearchItem) toUserTask(defaultState string) (ZeebeUserTask, error) {
	key, err := strconv.ParseInt(strings.TrimSpace(i.ID), 10, 64)
	if err != nil {
		return ZeebeUserTask{}, fmt.Errorf("task id: %w", err)
	}
	pik, err := strconv.ParseInt(strings.TrimSpace(i.ProcessInstanceKey), 10, 64)
	if err != nil {
		return ZeebeUserTask{}, fmt.Errorf("processInstanceKey: %w", err)
	}
	state := strings.TrimSpace(i.TaskState)
	if state == "" {
		state = defaultState
	}
	return ZeebeUserTask{
		UserTaskKey:        key,
		ElementID:          i.TaskDefinitionID,
		ProcessInstanceKey: pik,
		State:              state,
		CandidateGroups:    i.CandidateGroups,
		Assignee:           i.Assignee,
	}, nil
}

func (c *ZeebeRestClient) searchUserTasksViaGateway(ctx context.Context, processInstanceKey int64, state string) ([]ZeebeUserTask, error) {
	var req zeebeUserTaskSearchRequest
	req.Filter.ProcessInstanceKey = strconv.FormatInt(processInstanceKey, 10)
	req.Filter.State = state
	req.Page.From = 0
	req.Page.Limit = 50

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/user-tasks/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway search user tasks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway search user tasks HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed zeebeUserTaskSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode gateway user task search: %w", err)
	}
	out := make([]ZeebeUserTask, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		ut, err := item.toUserTask()
		if err != nil {
			slog.Warn("skip malformed gateway user task item", "err", err)
			continue
		}
		out = append(out, ut)
	}
	return out, nil
}

type httpStatusError struct {
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

func isHTTPNotFound(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound
}

func (i zeebeUserTaskItem) toUserTask() (ZeebeUserTask, error) {
	key, err := i.UserTaskKey.Int64()
	if err != nil {
		return ZeebeUserTask{}, fmt.Errorf("userTaskKey: %w", err)
	}
	pik, err := i.ProcessInstanceKey.Int64()
	if err != nil {
		return ZeebeUserTask{}, fmt.Errorf("processInstanceKey: %w", err)
	}
	return ZeebeUserTask{
		UserTaskKey:        key,
		ElementID:          i.ElementID,
		ProcessInstanceKey: pik,
		State:              i.State,
		CandidateGroups:    i.CandidateGroups,
		Assignee:           i.Assignee,
	}, nil
}

func (c *ZeebeRestClient) AssignUserTask(ctx context.Context, userTaskKey int64, assignee string) error {
	if !c.Enabled() {
		return fmt.Errorf("zeebe REST client is not configured")
	}
	payload, _ := json.Marshal(map[string]string{"assignee": assignee})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/user-tasks/%d/assignment", c.baseURL, userTaskKey),
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("assign user task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("assign user task HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *ZeebeRestClient) CompleteUserTask(ctx context.Context, userTaskKey int64, variables map[string]any) error {
	if !c.Enabled() {
		return fmt.Errorf("zeebe REST client is not configured")
	}
	payload, err := json.Marshal(map[string]any{"variables": variables})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/user-tasks/%d/completion", c.baseURL, userTaskKey),
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("complete user task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("complete user task HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// IsNativeUserTaskElement reports whether a BPMN element id denotes a v2 native user task.
func IsNativeUserTaskElement(elementID string) bool {
	return strings.HasPrefix(strings.TrimSpace(elementID), "UT_")
}
