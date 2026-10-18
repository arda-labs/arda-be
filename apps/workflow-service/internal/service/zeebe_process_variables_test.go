package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

const processInstanceKeyFixture = int64(2251799829066558)

// variableHit renders one zeebe-record_variable_* document the way the Zeebe
// 8.5 Elasticsearch exporter writes it: value.value is a JSON-encoded string.
func variableHit(position int64, intent, name, jsonValue string, scopeKey int64) string {
	encoded, _ := json.Marshal(jsonValue)
	return `{"_source":{"position":` + strconv.FormatInt(position, 10) +
		`,"timestamp":` + strconv.FormatInt(1760000000000+position, 10) +
		`,"intent":"` + intent + `","valueType":"VARIABLE","value":{"name":"` + name +
		`","value":` + string(encoded) +
		`,"scopeKey":` + strconv.FormatInt(scopeKey, 10) +
		`,"processInstanceKey":` + strconv.FormatInt(processInstanceKeyFixture, 10) + `}}}`
}

type capturedSearch struct {
	mu   sync.Mutex
	path string
	body string
}

func (c *capturedSearch) record(path, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path, c.body = path, body
}

func (c *capturedSearch) get() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path, c.body
}

func variablesESServer(t *testing.T, hits ...string) (*httptest.Server, *capturedSearch) {
	t.Helper()
	captured := &capturedSearch{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.record(r.URL.Path, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":{"hits":[` + strings.Join(hits, ",") + `]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func TestProcessVariablesLatestRecordWins(t *testing.T) {
	pik := processInstanceKeyFixture
	srv, _ := variablesESServer(t,
		variableHit(1, "CREATED", "caseCode", `"DPM-1"`, pik),
		variableHit(2, "CREATED", "amount", `100`, pik),
		variableHit(3, "UPDATED", "amount", `250`, pik),
	)
	vars, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), pik)
	if err != nil {
		t.Fatalf("ProcessVariables: %v", err)
	}
	if got := vars["amount"].(json.Number).String(); got != "250" {
		t.Fatalf("amount = %s, want the UPDATED value 250", got)
	}
	if vars["caseCode"] != "DPM-1" {
		t.Fatalf("caseCode = %v, want DPM-1", vars["caseCode"])
	}
}

func TestProcessVariablesOrdersByPositionNotResponseOrder(t *testing.T) {
	pik := processInstanceKeyFixture
	srv, _ := variablesESServer(t,
		variableHit(9, "UPDATED", "status", `"NEW"`, pik),
		variableHit(4, "CREATED", "status", `"OLD"`, pik),
	)
	vars, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), pik)
	if err != nil {
		t.Fatalf("ProcessVariables: %v", err)
	}
	if vars["status"] != "NEW" {
		t.Fatalf("status = %v, want NEW (highest position)", vars["status"])
	}
}

func TestProcessVariablesParsesJSONTypes(t *testing.T) {
	pik := processInstanceKeyFixture
	srv, _ := variablesESServer(t,
		variableHit(1, "CREATED", "text", `"xin chào"`, pik),
		variableHit(2, "CREATED", "count", `500000000`, pik),
		variableHit(3, "CREATED", "bigKey", `2251799829066558`, pik),
		variableHit(4, "CREATED", "flag", `true`, pik),
		variableHit(5, "CREATED", "nothing", `null`, pik),
		variableHit(6, "CREATED", "obj", `{"a":[1,2],"b":"c"}`, pik),
		variableHit(7, "CREATED", "garbled", `not-json`, pik),
	)
	vars, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), pik)
	if err != nil {
		t.Fatalf("ProcessVariables: %v", err)
	}
	if vars["text"] != "xin chào" {
		t.Fatalf("text = %#v", vars["text"])
	}
	if vars["count"].(json.Number).String() != "500000000" {
		t.Fatalf("count = %#v", vars["count"])
	}
	if vars["bigKey"].(json.Number).String() != "2251799829066558" {
		t.Fatalf("bigKey lost precision: %#v", vars["bigKey"])
	}
	if vars["flag"] != true {
		t.Fatalf("flag = %#v", vars["flag"])
	}
	if v, ok := vars["nothing"]; !ok || v != nil {
		t.Fatalf("nothing = %#v (present=%v), want present null", v, ok)
	}
	obj, ok := vars["obj"].(map[string]any)
	if !ok || obj["b"] != "c" {
		t.Fatalf("obj = %#v", vars["obj"])
	}
	if vars["garbled"] != "not-json" {
		t.Fatalf("unparsable value should fall back to the raw string, got %#v", vars["garbled"])
	}
}

func TestProcessVariablesOnlyProcessScope(t *testing.T) {
	pik := processInstanceKeyFixture
	childScope := pik + 50
	srv, _ := variablesESServer(t,
		variableHit(1, "CREATED", "shared", `"process"`, pik),
		variableHit(2, "CREATED", "shared", `"local"`, childScope),
		variableHit(3, "CREATED", "localOnly", `"x"`, childScope),
	)
	vars, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), pik)
	if err != nil {
		t.Fatalf("ProcessVariables: %v", err)
	}
	if vars["shared"] != "process" {
		t.Fatalf("shared = %v, want the process-scope value", vars["shared"])
	}
	if _, leaked := vars["localOnly"]; leaked {
		t.Fatalf("element-scoped variable leaked into process variables: %#v", vars)
	}
}

func TestProcessVariablesNoRecordsReturnsEmptyMap(t *testing.T) {
	srv, _ := variablesESServer(t)
	vars, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), processInstanceKeyFixture)
	if err != nil {
		t.Fatalf("ProcessVariables: %v", err)
	}
	if vars == nil || len(vars) != 0 {
		t.Fatalf("vars = %#v, want a non-nil empty map (marshals to {})", vars)
	}
}

func TestProcessVariablesQueriesVariableRecordsOfInstance(t *testing.T) {
	srv, captured := variablesESServer(t)
	if _, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), processInstanceKeyFixture); err != nil {
		t.Fatalf("ProcessVariables: %v", err)
	}
	path, body := captured.get()
	if !strings.HasSuffix(path, "/_search") {
		t.Fatalf("path = %q, want an ES _search", path)
	}
	for _, want := range []string{`"VARIABLE"`, `value.processInstanceKey`, `2251799829066558`, `"position"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("search body missing %s: %s", want, body)
		}
	}
}

func TestProcessVariablesErrors(t *testing.T) {
	pik := processInstanceKeyFixture
	t.Run("elasticsearch HTTP error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), pik)
		if !errors.Is(err, service.ErrZeebeVariablesUnavailable) || !strings.Contains(err.Error(), "HTTP 500") {
			t.Fatalf("err = %v, want ErrZeebeVariablesUnavailable mentioning HTTP 500", err)
		}
	})
	t.Run("elasticsearch unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, err := service.NewZeebeMonitoringIndex(url).ProcessVariables(context.Background(), pik)
		if !errors.Is(err, service.ErrZeebeVariablesUnavailable) {
			t.Fatalf("err = %v, want ErrZeebeVariablesUnavailable", err)
		}
	})
	t.Run("malformed search response is an error, not silent empty variables", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`<html>proxy error</html>`))
		}))
		defer srv.Close()
		_, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), pik)
		if !errors.Is(err, service.ErrZeebeVariablesUnavailable) {
			t.Fatalf("err = %v, want ErrZeebeVariablesUnavailable", err)
		}
	})
	t.Run("unconfigured index", func(t *testing.T) {
		_, err := service.NewZeebeMonitoringIndex("").ProcessVariables(context.Background(), pik)
		if !errors.Is(err, service.ErrZeebeVariablesUnavailable) {
			t.Fatalf("err = %v, want ErrZeebeVariablesUnavailable", err)
		}
	})
	t.Run("missing process instance key", func(t *testing.T) {
		srv, _ := variablesESServer(t)
		if _, err := service.NewZeebeMonitoringIndex(srv.URL).ProcessVariables(context.Background(), 0); err == nil {
			t.Fatal("expected an error for processInstanceKey 0")
		}
	})
}
