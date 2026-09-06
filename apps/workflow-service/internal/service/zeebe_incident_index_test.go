package service

import (
	"testing"
)

func TestOpenIncidentsFromESFoldsResolvedRecords(t *testing.T) {
	raw := []byte(`{
		"hits": {"hits": [
			{"_source": {"valueType": "INCIDENT", "intent": "CREATED", "value": {
				"incidentKey": 11, "processInstanceKey": 99, "processDefinitionKey": 5,
				"elementInstanceKey": 77, "elementId": "ServiceTask_CallFAC",
				"jobKey": 42, "errorType": "JOB_NO_RETRIES", "errorMessage": "connect refused",
				"creationTime": "2026-09-06T01:00:00.000Z"}}},
			{"_source": {"valueType": "INCIDENT", "intent": "RESOLVED", "value": {"incidentKey": 11}}},
			{"_source": {"valueType": "INCIDENT", "intent": "CREATED", "value": {
				"incidentKey": 12, "processInstanceKey": 99, "elementId": "ServiceTask_Interest",
				"errorType": "IO_MAPPING_ERROR", "errorMessage": "missing variable"}}}
		]}}
	`)
	incidents := OpenIncidentsFromESForTest(raw)
	if len(incidents) != 1 {
		t.Fatalf("expected only the still-open incident, got %d: %+v", len(incidents), incidents)
	}
	inc := incidents[0]
	if inc.IncidentKey != 12 || inc.ElementID != "ServiceTask_Interest" || inc.ErrorType != "IO_MAPPING_ERROR" {
		t.Fatalf("unexpected folded incident: %+v", inc)
	}
}

func TestOpenIncidentsFromESIgnoresOtherValueTypes(t *testing.T) {
	raw := []byte(`{"hits": {"hits": [
		{"_source": {"valueType": "USER_TASK", "intent": "CREATED", "value": {"userTaskKey": 1}}},
		{"_source": {"valueType": "INCIDENT", "intent": "RESOLVED", "value": {"incidentKey": 3}}}
	]}}`)
	if incidents := OpenIncidentsFromESForTest(raw); len(incidents) != 0 {
		t.Fatalf("expected no open incidents, got %+v", incidents)
	}
}
