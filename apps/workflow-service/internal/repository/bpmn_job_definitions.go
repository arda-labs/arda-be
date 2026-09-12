package repository

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// BPMNJobDefinition is one zeebe:taskDefinition declared in a BPMN process.
type BPMNJobDefinition struct {
	ProcessID   string
	ElementID   string
	ElementName string
	Type        string
	Retries     string
}

type bpmnTaskElement struct {
	ID                string `xml:"id,attr"`
	Name              string `xml:"name,attr"`
	ExtensionElements struct {
		TaskDefinition *struct {
			Type    string `xml:"type,attr"`
			Retries string `xml:"retries,attr"`
		} `xml:"taskDefinition"`
	} `xml:"extensionElements"`
}

type bpmnDefinitionsDocument struct {
	Processes []struct {
		ID           string            `xml:"id,attr"`
		ServiceTasks []bpmnTaskElement `xml:"serviceTask"`
		SendTasks    []bpmnTaskElement `xml:"sendTask"`
	} `xml:"process"`
}

// ExtractBPMNJobDefinitions flattens the service/send tasks of a BPMN document
// that declare a zeebe:taskDefinition type. Tasklist-style user tasks carry no
// job type and are intentionally excluded.
func ExtractBPMNJobDefinitions(content []byte) ([]BPMNJobDefinition, error) {
	content = bytes.TrimSpace(content)
	if len(content) == 0 {
		return nil, fmt.Errorf("empty BPMN XML")
	}
	var doc bpmnDefinitionsDocument
	if err := xml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("invalid BPMN XML: %w", err)
	}

	out := make([]BPMNJobDefinition, 0)
	for _, process := range doc.Processes {
		tasks := append(append([]bpmnTaskElement{}, process.ServiceTasks...), process.SendTasks...)
		for _, task := range tasks {
			if task.ExtensionElements.TaskDefinition == nil {
				continue
			}
			jobType := strings.TrimSpace(task.ExtensionElements.TaskDefinition.Type)
			if jobType == "" {
				continue
			}
			out = append(out, BPMNJobDefinition{
				ProcessID:   strings.TrimSpace(process.ID),
				ElementID:   strings.TrimSpace(task.ID),
				ElementName: strings.TrimSpace(task.Name),
				Type:        jobType,
				Retries:     strings.TrimSpace(task.ExtensionElements.TaskDefinition.Retries),
			})
		}
	}
	return out, nil
}
