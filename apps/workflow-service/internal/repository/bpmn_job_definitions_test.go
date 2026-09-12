package repository

import "testing"

func TestExtractBPMNJobDefinitions(t *testing.T) {
	content := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
	xmlns:zeebe="http://camunda.org/schema/zeebe/1.0">
	<bpmn:process id="crm-reg">
		<bpmn:serviceTask id="ST_Execute" name="Execute registration">
			<bpmn:extensionElements>
				<zeebe:taskDefinition type="crm.customer.register.execute" retries="5" />
			</bpmn:extensionElements>
		</bpmn:serviceTask>
		<bpmn:serviceTask id="ST_NoType" name="No job type" />
		<bpmn:userTask id="UT_Review" name="Review" />
	</bpmn:process>
</bpmn:definitions>`)

	jobs, err := ExtractBPMNJobDefinitions(content)
	if err != nil {
		t.Fatalf("ExtractBPMNJobDefinitions: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job definition, got %d: %+v", len(jobs), jobs)
	}
	job := jobs[0]
	if job.Type != "crm.customer.register.execute" || job.ElementID != "ST_Execute" ||
		job.ElementName != "Execute registration" || job.Retries != "5" || job.ProcessID != "crm-reg" {
		t.Fatalf("unexpected job definition: %+v", job)
	}
}

func TestExtractBPMNJobDefinitionsRejectsInvalidXML(t *testing.T) {
	if _, err := ExtractBPMNJobDefinitions([]byte("<not-xml")); err == nil {
		t.Fatal("expected an error for malformed XML")
	}
}
