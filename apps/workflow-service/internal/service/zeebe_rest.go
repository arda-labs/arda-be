package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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

type ZeebeRestClient struct {
	baseURL    string
	esIndex    *ZeebeUserTaskIndex
	httpClient *http.Client
}

func NewZeebeRestClient(restAddr, _ string, esIndex *ZeebeUserTaskIndex) *ZeebeRestClient {
	restAddr = normalizeHTTPBaseURL(restAddr)
	if restAddr == "" {
		return nil
	}
	return &ZeebeRestClient{
		baseURL: restAddr,
		esIndex: esIndex,
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

// DeriveZeebeTasklistAddr is kept for config compatibility; v2 runtime does not call Tasklist.
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
	if c.esIndex != nil && c.esIndex.Enabled() {
		return c.esIndex.SearchUserTasks(ctx, processInstanceKey, state)
	}
	return nil, fmt.Errorf(
		"zeebe user task search requires ZEEBE_ES_URL (Elasticsearch exporter); Camunda 8.5 gateway REST has no list/search API",
	)
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
