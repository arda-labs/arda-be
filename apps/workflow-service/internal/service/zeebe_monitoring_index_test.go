package service_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

func TestProcessInstanceStateFolding(t *testing.T) {
	cases := map[string]string{
		"ELEMENT_ACTIVATING":  "ACTIVE",
		"ELEMENT_ACTIVATED":   "ACTIVE",
		"ELEMENT_COMPLETING":  "COMPLETED",
		"ELEMENT_COMPLETED":   "COMPLETED",
		"ELEMENT_TERMINATING": "TERMINATED",
		"ELEMENT_TERMINATED":  "TERMINATED",
	}
	for intent, want := range cases {
		if got := service.ProcessInstanceStateForTest(intent); got != want {
			t.Fatalf("ProcessInstanceStateForTest(%q) = %q, want %q", intent, got, want)
		}
	}
}

func TestFoldProcessInstance(t *testing.T) {
	first := []byte(`{"position":1,"key":225,"timestamp":1789178600000,"intent":"ELEMENT_ACTIVATED","valueType":"PROCESS_INSTANCE",
		"value":{"processInstanceKey":225,"processDefinitionKey":5,"bpmnProcessId":"crm-reg","version":3,
		"elementId":"crm-reg","bpmnElementType":"PROCESS","flowScopeKey":-1,"parentProcessInstanceKey":-1}}`)
	latest := []byte(`{"position":9,"key":225,"timestamp":1789178700000,"intent":"ELEMENT_COMPLETED","valueType":"PROCESS_INSTANCE",
		"value":{"processInstanceKey":225,"bpmnProcessId":"crm-reg","elementId":"crm-reg","bpmnElementType":"PROCESS"}}`)

	pi, ok := service.FoldProcessInstanceForTest(225, first, latest)
	if !ok {
		t.Fatal("expected the instance to fold")
	}
	if pi.ProcessInstanceKey != "225" || pi.BpmnProcessID != "crm-reg" || pi.Version != 3 {
		t.Fatalf("unexpected instance identity: %+v", pi)
	}
	if pi.State != "COMPLETED" || pi.EndTime == "" {
		t.Fatalf("expected COMPLETED with end time, got %+v", pi)
	}
	if pi.StartTime == "" {
		t.Fatal("expected start time from the first record")
	}
	if pi.ParentProcessInstanceKey != "" {
		t.Fatalf("expected no parent, got %q", pi.ParentProcessInstanceKey)
	}
}

func TestElementInstancesFromES(t *testing.T) {
	raw := []byte(`{"hits":{"hits":[
		{"_source":{"key":11,"timestamp":1000,"intent":"ELEMENT_ACTIVATED","valueType":"PROCESS_INSTANCE",
			"value":{"processInstanceKey":9,"elementId":"Task_A","bpmnElementType":"SERVICE_TASK","flowScopeKey":9}}},
		{"_source":{"key":11,"timestamp":2000,"intent":"ELEMENT_COMPLETED","valueType":"PROCESS_INSTANCE",
			"value":{"processInstanceKey":9,"elementId":"Task_A","bpmnElementType":"SERVICE_TASK","flowScopeKey":9}}},
		{"_source":{"key":12,"timestamp":1500,"intent":"ELEMENT_ACTIVATED","valueType":"PROCESS_INSTANCE",
			"value":{"processInstanceKey":9,"elementId":"Task_B","bpmnElementType":"USER_TASK","flowScopeKey":9}}}
	]}}`)

	items := service.ElementInstancesFromESForTest(raw)
	if len(items) != 2 {
		t.Fatalf("expected 2 element instances, got %d", len(items))
	}
	byID := map[string]service.ZeebeElementInstance{}
	for _, item := range items {
		byID[item.ElementID] = item
	}
	if byID["Task_A"].State != "COMPLETED" || byID["Task_A"].EndTime == "" {
		t.Fatalf("unexpected Task_A fold: %+v", byID["Task_A"])
	}
	if byID["Task_B"].State != "ACTIVE" || byID["Task_B"].EndTime != "" {
		t.Fatalf("unexpected Task_B fold: %+v", byID["Task_B"])
	}
	if byID["Task_A"].ElementInstanceKey != "11" || byID["Task_B"].ElementInstanceKey != "12" {
		t.Fatalf("element keys must come from the record key: %+v", items)
	}
}

func TestVariablesFromESLatestWinsPerScope(t *testing.T) {
	raw := []byte(`{"hits":{"hits":[
		{"_source":{"timestamp":1000,"intent":"CREATED","valueType":"VARIABLE",
			"value":{"name":"amount","value":"100","scopeKey":9,"processInstanceKey":9}}},
		{"_source":{"timestamp":1500,"intent":"UPDATED","valueType":"VARIABLE",
			"value":{"name":"amount","value":"250","scopeKey":9,"processInstanceKey":9}}},
		{"_source":{"timestamp":1200,"intent":"CREATED","valueType":"VARIABLE",
			"value":{"name":"amount","value":"7","scopeKey":11,"processInstanceKey":9}}}
	]}}`)

	items := service.VariablesFromESForTest(raw)
	if len(items) != 2 {
		t.Fatalf("expected 2 scope entries, got %d", len(items))
	}
	values := map[string]string{}
	for _, item := range items {
		values[item.ScopeKey] = item.Value
	}
	if values["9"] != "250" || values["11"] != "7" {
		t.Fatalf("unexpected folded variables: %+v", values)
	}
}

func TestJobsFromESFoldsStateByIntent(t *testing.T) {
	raw := []byte(`{"hits":{"hits":[
		{"_source":{"key":77,"timestamp":1000,"intent":"CREATED","valueType":"JOB",
			"value":{"type":"crm.sync","retries":3,"worker":"","elementId":"Task_A","processInstanceKey":9}}},
		{"_source":{"key":77,"timestamp":2000,"intent":"FAILED","valueType":"JOB",
			"value":{"type":"crm.sync","retries":0,"worker":"default","elementId":"Task_A","processInstanceKey":9,"errorMessage":"boom"}}},
		{"_source":{"key":77,"timestamp":2500,"intent":"RETRIES_UPDATED","valueType":"JOB",
			"value":{"type":"crm.sync","retries":3,"worker":"default","elementId":"Task_A","processInstanceKey":9}}}
	]}}`)

	items := service.JobsFromESForTest(raw)
	if len(items) != 1 {
		t.Fatalf("expected 1 folded job, got %d", len(items))
	}
	job := items[0]
	if job.JobKey != "77" || job.State != "ACTIVATABLE" || job.Retries != 3 {
		t.Fatalf("unexpected job fold: %+v", job)
	}
	if job.ErrorMessage != "boom" || job.CreatedAt == "" {
		t.Fatalf("expected error message and created time: %+v", job)
	}
}

func TestOpenIncidentsFromESUsesRecordKeyOn85(t *testing.T) {
	raw := []byte(`{"hits":{"hits":[
		{"_source":{"key":900,"timestamp":1000,"intent":"CREATED","valueType":"INCIDENT",
			"value":{"processInstanceKey":9,"elementId":"Task_A","jobKey":77,"errorType":"JOB_NO_RETRIES","errorMessage":"boom","bpmnProcessId":"crm-reg"}}},
		{"_source":{"key":900,"timestamp":2000,"intent":"RESOLVED","valueType":"INCIDENT","value":{}}}
	]}}`)

	open := service.OpenIncidentsFromESForTest(raw)
	if len(open) != 0 {
		t.Fatalf("expected resolved incident to be folded out, got %+v", open)
	}

	raw = []byte(`{"hits":{"hits":[
		{"_source":{"key":901,"timestamp":1000,"intent":"CREATED","valueType":"INCIDENT",
			"value":{"processInstanceKey":9,"elementId":"Task_A","jobKey":77,"errorType":"JOB_NO_RETRIES","errorMessage":"boom"}}}
	]}}`)
	open = service.OpenIncidentsFromESForTest(raw)
	if len(open) != 1 || open[0].IncidentKey != 901 || open[0].JobKey != 77 || open[0].BpmnProcessID != "" {
		t.Fatalf("unexpected open incident: %+v", open)
	}
}

func TestSearchProcessInstancesPostFiltersAndCursors(t *testing.T) {
	activeFirst := `{"key":101,"timestamp":1000,"intent":"ELEMENT_ACTIVATED","valueType":"PROCESS_INSTANCE",
		"value":{"processInstanceKey":101,"processDefinitionKey":5,"bpmnProcessId":"p1","version":1,"elementId":"p1","bpmnElementType":"PROCESS"}}`
	activeLatest := `{"key":101,"timestamp":1500,"intent":"ELEMENT_ACTIVATED","valueType":"PROCESS_INSTANCE",
		"value":{"processInstanceKey":101,"bpmnProcessId":"p1","elementId":"p1","bpmnElementType":"PROCESS"}}`
	doneFirst := `{"key":100,"timestamp":900,"intent":"ELEMENT_ACTIVATED","valueType":"PROCESS_INSTANCE",
		"value":{"processInstanceKey":100,"processDefinitionKey":5,"bpmnProcessId":"p1","version":2,"elementId":"p1","bpmnElementType":"PROCESS"}}`
	doneLatest := `{"key":100,"timestamp":2000,"intent":"ELEMENT_COMPLETED","valueType":"PROCESS_INSTANCE",
		"value":{"processInstanceKey":100,"bpmnProcessId":"p1","elementId":"p1","bpmnElementType":"PROCESS"}}`

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &requestBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"aggregations":{"entities":{"buckets":[
			{"key":{"entityKey":101},"rootFirst":{"hit":{"hits":{"hits":[{"_source":` + activeFirst + `}]}}},"rootLatest":{"hit":{"hits":{"hits":[{"_source":` + activeLatest + `}]}}}},
			{"key":{"entityKey":100},"rootFirst":{"hit":{"hits":{"hits":[{"_source":` + doneFirst + `}]}}},"rootLatest":{"hit":{"hits":{"hits":[{"_source":` + doneLatest + `}]}}}}
		]}}}`))
	}))
	defer server.Close()

	index := service.NewZeebeMonitoringIndex(server.URL)
	items, cursor, err := index.SearchProcessInstances(context.Background(), service.ProcessInstanceSearchParams{
		State:    "COMPLETED",
		PageSize: 5,
	})
	if err != nil {
		t.Fatalf("SearchProcessInstances: %v", err)
	}
	if len(items) != 1 || items[0].ProcessInstanceKey != "100" {
		t.Fatalf("expected only the completed instance, got %+v", items)
	}
	if cursor != "" {
		t.Fatalf("expected no cursor at the end of results, got %q", cursor)
	}

	firstPage, next, err := index.SearchProcessInstances(context.Background(), service.ProcessInstanceSearchParams{
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("SearchProcessInstances page: %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].ProcessInstanceKey != "101" || next != "101" {
		t.Fatalf("expected the newest instance and a cursor, got %+v cursor=%q", firstPage, next)
	}

	query, _ := requestBody["query"].(map[string]any)
	if query == nil {
		t.Fatalf("expected a query body, got %+v", requestBody)
	}
	serialized, _ := json.Marshal(requestBody)
	if !strings.Contains(string(serialized), "PROCESS_INSTANCE") {
		t.Fatalf("expected the query to filter by valueType: %s", serialized)
	}
}

func TestSearchIncidentsFoldsDeletedStatesAndFiltersOpen(t *testing.T) {
	created := `{"key":900,"timestamp":1000,"intent":"CREATED","valueType":"INCIDENT",
		"value":{"processInstanceKey":9,"elementId":"Task_A","jobKey":77,"errorType":"JOB_NO_RETRIES","errorMessage":"boom"}}`
	resolvedLatest := `{"key":900,"timestamp":2000,"intent":"RESOLVED","valueType":"INCIDENT","value":{}}`
	openCreated := `{"key":901,"timestamp":3000,"intent":"CREATED","valueType":"INCIDENT",
		"value":{"processInstanceKey":10,"elementId":"Task_B","jobKey":88,"errorType":"IO_MAPPING_ERROR","errorMessage":"missing"}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"aggregations":{"entities":{"buckets":[
			{"key":{"entityKey":900},
			 "latest":{"hits":{"hits":[{"_source":` + resolvedLatest + `}]}},
			 "created":{"hit":{"hits":{"hits":[{"_source":` + created + `}]}}}},
			{"key":{"entityKey":901},
			 "latest":{"hits":{"hits":[{"_source":` + openCreated + `}]}},
			 "created":{"hit":{"hits":{"hits":[{"_source":` + openCreated + `}]}}}}
		]}}}`))
	}))
	defer server.Close()

	index := service.NewZeebeMonitoringIndex(server.URL)
	items, cursor, err := index.SearchIncidents(context.Background(), service.IncidentSearchParams{
		State:    "CREATED",
		PageSize: 5,
	})
	if err != nil {
		t.Fatalf("SearchIncidents: %v", err)
	}
	if len(items) != 1 || items[0].IncidentKey != 901 || items[0].JobKey != 88 {
		t.Fatalf("expected only the open incident, got %+v", items)
	}
	if items[0].State != "CREATED" || items[0].BpmnProcessID != "" {
		t.Fatalf("unexpected incident state: %+v", items[0])
	}
	if cursor != "" {
		t.Fatalf("expected no cursor at the end of results, got %q", cursor)
	}
}
