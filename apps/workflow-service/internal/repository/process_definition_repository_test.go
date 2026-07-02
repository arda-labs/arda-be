package repository

import "testing"

func TestExtractBPMNProcessID(t *testing.T) {
	xml := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="customer-registration-v1" isExecutable="true" />
</bpmn:definitions>`)

	got, err := ExtractBPMNProcessID(xml)
	if err != nil {
		t.Fatalf("ExtractBPMNProcessID() error = %v", err)
	}
	if got != "customer-registration-v1" {
		t.Fatalf("ExtractBPMNProcessID() = %q", got)
	}
}

func TestExtractBPMNProcessIDRejectsInvalidXML(t *testing.T) {
	if _, err := ExtractBPMNProcessID([]byte(`<xml>`)); err == nil {
		t.Fatal("ExtractBPMNProcessID() expected an error")
	}
}
