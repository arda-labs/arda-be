package events

import (
	"encoding/json"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/service"
)

type WorkflowCaseEvent struct {
	EventID            string   `json:"event_id"`
	TenantID           string   `json:"tenant_id"`
	CaseID             string   `json:"case_id"`
	CaseType           string   `json:"case_type"`
	Status             string   `json:"status"`
	Href               string   `json:"href"`
	Locale             string   `json:"locale"`
	UserIDs            []string `json:"user_ids"`
	GroupIDs           []string `json:"group_ids"`
	RoleCodes          []string `json:"role_codes"`
	OrgUnitIDs         []string `json:"org_unit_ids"`
	IncludeDescendants bool     `json:"include_descendants"`
}

func WorkflowCaseInput(payload []byte, subject, defaultLocale string) (service.AcceptInput, error) {
	var event WorkflowCaseEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return service.AcceptInput{}, err
	}
	locale, err := ResolveLocale("", event.Locale, defaultLocale)
	if err != nil {
		return service.AcceptInput{}, err
	}
	eventType := strings.TrimPrefix(subject, "arda.")
	return businessInput(event.TenantID, event.EventID, "workflow-service", eventType, "case", event.CaseID, locale, event.CaseType, "", event.Href, event.UserIDs), nil
}
