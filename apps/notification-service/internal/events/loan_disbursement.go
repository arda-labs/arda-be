package events

import (
	"encoding/json"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/service"
)

type LoanDisbursementEvent struct {
	EventID            string   `json:"event_id"`
	TenantID           string   `json:"tenant_id"`
	DisbursementID     string   `json:"disbursement_id"`
	Status             string   `json:"status"`
	Href               string   `json:"href"`
	Locale             string   `json:"locale"`
	UserIDs            []string `json:"user_ids"`
	GroupIDs           []string `json:"group_ids"`
	RoleCodes          []string `json:"role_codes"`
	OrgUnitIDs         []string `json:"org_unit_ids"`
	IncludeDescendants bool     `json:"include_descendants"`
}

func LoanDisbursementInput(payload []byte, subject, defaultLocale string) (service.AcceptInput, error) {
	var event LoanDisbursementEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return service.AcceptInput{}, err
	}
	locale, err := ResolveLocale("", event.Locale, defaultLocale)
	if err != nil {
		return service.AcceptInput{}, err
	}
	eventType := strings.TrimPrefix(subject, "arda.")
	return businessInput(event.TenantID, event.EventID, "loan-service", eventType, "disbursement", event.DisbursementID, locale, event.Status, "", event.Href, event.UserIDs), nil
}
