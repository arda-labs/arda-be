package events

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
)

const (
	WorkflowTaskAssignedSubject   = "arda.workflow.task.assigned.v1"
	WorkflowTaskOverdueSubject    = "arda.workflow.task.overdue.v1"
	WorkflowTaskCompletedSubject  = "arda.workflow.task.completed.v1"
	WorkflowTaskReassignedSubject = "arda.workflow.task.reassigned.v1"
	WorkflowTaskSLAWarningSubject = "arda.workflow.task.sla_warning.v1"
)

type WorkflowTaskEvent struct {
	EventID            string   `json:"event_id"`
	TenantID           string   `json:"tenant_id"`
	TaskID             string   `json:"task_id"`
	UserIDs            []string `json:"user_ids"`
	TaskType           string   `json:"task_type"`
	Title              string   `json:"title"`
	Href               string   `json:"href"`
	Locale             string   `json:"locale"`
	GroupIDs           []string `json:"group_ids"`
	RoleCodes          []string `json:"role_codes"`
	OrgUnitIDs         []string `json:"org_unit_ids"`
	IncludeDescendants bool     `json:"include_descendants"`
	SLAMilestone       int      `json:"sla_milestone,omitempty"`
	SLAEvent           string   `json:"sla_event,omitempty"`
}

func WorkflowTaskInput(payload []byte, subject string, recipients []string, defaultLocale string) (service.AcceptInput, error) {
	var event WorkflowTaskEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return service.AcceptInput{}, err
	}
	locale, err := ResolveLocale("", event.Locale, defaultLocale)
	if err != nil {
		return service.AcceptInput{}, err
	}
	return businessInput(event.TenantID, event.EventID, "workflow-service", strings.TrimPrefix(subject, "arda."), "task", event.TaskID, locale, event.TaskType, event.Title, event.Href, recipients), nil
}

func ResolveLocale(preferenceLocale, envelopeLocale, defaultLocale string) (string, error) {
	for _, candidate := range []string{preferenceLocale, envelopeLocale, defaultLocale} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "vi-VN" || candidate == "en-US" {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("notification.locale_unavailable: no supported user, envelope, or configured default locale")
}

func businessInput(tenantID, eventID, source, eventType, entityType, entityID, locale, taskType, title, href string, userIDs []string) service.AcceptInput {
	key := strings.TrimSuffix(eventType, ".v1")
	params := map[string]any{"taskType": taskType, "title": strings.TrimSpace(title)}
	recipients := make([]domain.Recipient, 0, len(userIDs))
	for _, id := range cleanUserIDs(userIDs) {
		recipients = append(recipients, domain.Recipient{Type: "user", UserID: id})
	}
	return service.AcceptInput{TenantID: strings.TrimSpace(tenantID), IdempotencyKey: eventType + ":" + strings.TrimSpace(eventID), SourceService: source, SourceEventID: strings.TrimSpace(eventID), EventType: eventType, TemplateKey: key, Channels: []string{domain.ChannelInApp}, Recipients: recipients, Payload: params, Params: params, Type: "info", TitleKey: key + ".title", BodyKey: key + ".body", Href: strings.TrimSpace(href), EntityType: entityType, EntityID: strings.TrimSpace(entityID), DedupeKey: eventType + ":" + strings.TrimSpace(eventID), Locale: locale}
}
