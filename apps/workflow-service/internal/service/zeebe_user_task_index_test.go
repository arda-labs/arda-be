package service_test

import (
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

func TestDeriveZeebeESAddr(t *testing.T) {
	got := service.DeriveZeebeESAddr("zeebe-zeebe-gateway.platform.svc.cluster.local:26500")
	want := "http://zeebe-elasticsearch.platform.svc.cluster.local:9200"
	if got != want {
		t.Fatalf("DeriveZeebeESAddr() = %q, want %q", got, want)
	}
}

func TestActiveUserTasksFromES(t *testing.T) {
	raw := []byte(`{
	  "hits": {
	    "hits": [
	      {"_source": {
	        "valueType": "USER_TASK",
	        "intent": "CREATED",
	        "value": {
	          "userTaskKey": 1001,
	          "elementId": "UT_CheckerReview",
	          "processInstanceKey": 9001,
	          "candidateGroupsList": ["CUSTOMER_CHECKER"]
	        }
	      }},
	      {"_source": {
	        "valueType": "USER_TASK",
	        "intent": "COMPLETED",
	        "value": {
	          "userTaskKey": 1001,
	          "elementId": "UT_CheckerReview",
	          "processInstanceKey": 9001
	        }
	      }},
	      {"_source": {
	        "valueType": "USER_TASK",
	        "intent": "CREATED",
	        "value": {
	          "userTaskKey": 1002,
	          "elementId": "UT_MakerRevise",
	          "processInstanceKey": 9001,
	          "candidateGroupsList": ["CUSTOMER_MAKER"]
	        }
	      }}
	    ]
	  }
	}`)
	tasks, err := service.ActiveUserTasksFromESForTest(raw, "CREATED")
	if err != nil {
		t.Fatalf("ActiveUserTasksFromESForTest: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 active task, got %d", len(tasks))
	}
	if tasks[0].UserTaskKey != 1002 || tasks[0].ElementID != "UT_MakerRevise" {
		t.Fatalf("unexpected active task: %+v", tasks[0])
	}
}
