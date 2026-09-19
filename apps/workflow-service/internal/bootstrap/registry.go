package bootstrap

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// RegistryWriter persists the derived registry (implemented by the case
// repository).
type RegistryWriter interface {
	ListActiveOperationTypes(ctx context.Context) ([]repository.OperationTypeRef, error)
	UpsertCaseTypeSteps(ctx context.Context, caseType string, version int, steps []repository.CaseTypeStep) error
}

// SeedRegistry derives and persists the step registry for every ACTIVE case
// type from the embedded BPMN corpus. It fails when an active case type has no
// embedded process or no human steps: a case type without registry rows cannot
// be discovered by the projector, so startup must not silently continue.
func SeedRegistry(ctx context.Context, writer RegistryWriter, processes []Process) error {
	if writer == nil {
		return fmt.Errorf("registry writer is required")
	}
	contentByProcessID := make(map[string][]byte, len(processes))
	for _, process := range processes {
		id, err := ProcessIDOf(process.Content)
		if err != nil {
			return fmt.Errorf("embedded %s: %w", process.ResourceName, err)
		}
		contentByProcessID[id] = process.Content
	}
	refs, err := writer.ListActiveOperationTypes(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		content, ok := contentByProcessID[ref.BpmnProcessID]
		if !ok {
			return fmt.Errorf("active case type %s references %s but no embedded BPMN matches",
				ref.CaseType, ref.BpmnProcessID)
		}
		derived, err := DeriveRegistrySteps(ref.BpmnProcessID, content)
		if err != nil {
			return fmt.Errorf("derive steps for %s: %w", ref.CaseType, err)
		}
		steps := make([]repository.CaseTypeStep, 0, len(derived))
		for _, step := range derived {
			steps = append(steps, repository.CaseTypeStep{
				ElementID:         step.ElementID,
				StepCode:          step.StepCode,
				StepKind:          string(step.Kind),
				FormKey:           FormKey(ref.CaseType, step.StepCode),
				AllowedActions:    step.AllowedActions,
				RequiredCommentOn: step.RequiredCommentOn,
				SortOrder:         step.SortOrder,
			})
		}
		if err := writer.UpsertCaseTypeSteps(ctx, ref.CaseType, RegistryVersion, steps); err != nil {
			return fmt.Errorf("seed registry for %s: %w", ref.CaseType, err)
		}
	}
	return nil
}

// RegistryVersion is the case-type step registry version derived from the
// embedded BPMN corpus. Cases pin the version they started with; bump this
// constant when step metadata changes (new rows are inserted, old rows stay).
const RegistryVersion = 1

// StepKind classifies a human step for the shared task UI and action policy.
type StepKind string

const (
	StepKindInput   StepKind = "INPUT"
	StepKindRevise  StepKind = "REVISE"
	StepKindChecker StepKind = "CHECKER"
)

// RegistryStep is one human step derived from a BPMN user task.
type RegistryStep struct {
	ElementID         string
	StepCode          string
	Kind              StepKind
	AllowedActions    []string
	RequiredCommentOn []string
	SortOrder         int
}

// Action constants shared with the HTTP complete handler.
const (
	ActionSubmit         = "SUBMIT"
	ActionApprove        = "APPROVE"
	ActionRequestChanges = "REQUEST_CHANGES"
	ActionReject         = "REJECT"
)

// DeriveRegistrySteps parses one BPMN document and derives the human-step
// metadata for the registry. The rules are deliberately code-owned and
// testable: step kind from the element id, allowed actions from the kind plus
// per-process overrides (loan formation TW/PGD have no REJECT branch).
func DeriveRegistrySteps(processID string, content []byte) ([]RegistryStep, error) {
	proc, err := parseProcess(content)
	if err != nil {
		return nil, err
	}
	if proc.ID == "" {
		return nil, fmt.Errorf("bpmn process id is missing")
	}
	if processID != "" && proc.ID != processID {
		return nil, fmt.Errorf("bpmn process id %q does not match %q", proc.ID, processID)
	}
	steps := make([]RegistryStep, 0, len(proc.UserTasks))
	for i, ut := range proc.UserTasks {
		elementID := strings.TrimSpace(ut.ID)
		if elementID == "" {
			continue
		}
		kind := deriveStepKind(elementID)
		stepCode := elementID
		for _, header := range ut.ExtensionElements.TaskHeaders {
			if strings.EqualFold(strings.TrimSpace(header.Key), "stepCode") &&
				strings.TrimSpace(header.Value) != "" {
				stepCode = strings.TrimSpace(header.Value)
				break
			}
		}
		steps = append(steps, RegistryStep{
			ElementID:         elementID,
			StepCode:          stepCode,
			Kind:              kind,
			AllowedActions:    allowedActions(proc.ID, elementID, kind),
			RequiredCommentOn: []string{ActionRequestChanges, ActionReject},
			SortOrder:         i + 1,
		})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("process %q has no human user tasks", proc.ID)
	}
	return steps, nil
}

// ProcessIDOf returns the bpmn:process id of an embedded BPMN document.
func ProcessIDOf(content []byte) (string, error) {
	proc, err := parseProcess(content)
	if err != nil {
		return "", err
	}
	return proc.ID, nil
}

// FormKey is the stable key a domain remote registers its step component
// under. It is case-type specific because two case types can share one
// process (e.g. dpm-rate-v1 serves register and edit).
func FormKey(caseType, stepCode string) string {
	return strings.ToLower(strings.TrimSpace(caseType)) + "." + strings.ToLower(strings.TrimSpace(stepCode))
}

func deriveStepKind(elementID string) StepKind {
	lower := strings.ToLower(elementID)
	switch {
	case strings.Contains(lower, "makerrevise"):
		return StepKindRevise
	case strings.Contains(lower, "makerinput"), strings.HasSuffix(lower, "_maker"):
		return StepKindInput
	default:
		return StepKindChecker
	}
}

// allowedActions returns the actions a step accepts. Checker steps default to
// approve/request-changes/reject; loan formation's TW and PGD reviews have no
// reject branch in the BPMN (a REJECT decision falls through to supplement),
// so they only expose approve/request-changes.
func allowedActions(processID, elementID string, kind StepKind) []string {
	if kind != StepKindChecker {
		return []string{ActionSubmit}
	}
	switch processID {
	case "lnm-loan-formation-v2":
		switch elementID {
		case "UT_TWRevalidate", "UT_PGDReview":
			return []string{ActionApprove, ActionRequestChanges}
		}
	}
	return []string{ActionApprove, ActionRequestChanges, ActionReject}
}

type bpmnDefinitions struct {
	Processes []bpmnProcess `xml:"process"`
}

type bpmnProcess struct {
	ID        string         `xml:"id,attr"`
	UserTasks []bpmnUserTask `xml:"userTask"`
}

type bpmnUserTask struct {
	ID                string `xml:"id,attr"`
	ExtensionElements struct {
		TaskHeaders []struct {
			Key   string `xml:"key,attr"`
			Value string `xml:"value,attr"`
		} `xml:"taskHeaders>header"`
		Assignment *struct {
			CandidateGroups string `xml:"candidateGroups,attr"`
		} `xml:"assignmentDefinition"`
	} `xml:"extensionElements"`
}

func parseProcess(content []byte) (bpmnProcess, error) {
	var defs bpmnDefinitions
	if err := xml.Unmarshal(content, &defs); err != nil {
		return bpmnProcess{}, fmt.Errorf("parse bpmn: %w", err)
	}
	if len(defs.Processes) == 0 {
		return bpmnProcess{}, fmt.Errorf("bpmn document has no process")
	}
	return defs.Processes[0], nil
}
