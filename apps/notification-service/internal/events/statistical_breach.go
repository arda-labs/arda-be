package events

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
)

// StatisticalBreachSubject is the subject statistical-service publishes a
// breached indicator on (rpt_outbox_events -> outbox relay -> JetStream).
const StatisticalBreachSubject = "arda.statistical.indicator.breached.v1"

// StatisticalBreachEvent is the outbox payload statistical-service writes.
// Only the fields the notification needs are decoded.
type StatisticalBreachEvent struct {
	AlertID       string   `json:"alert_id"`
	TenantID      string   `json:"tenant_id"`
	RuleCode      string   `json:"rule_code"`
	RuleName      string   `json:"rule_name"`
	OwnerUserID   string   `json:"owner_user_id"`
	IndicatorCode string   `json:"indicator_code"`
	PeriodCode    string   `json:"period_code"`
	DimensionKey  string   `json:"dimension_key"`
	Value         *float64 `json:"value"`
	Threshold     float64  `json:"threshold"`
	Operator      string   `json:"operator"`
	Severity      string   `json:"severity"`
	Message       string   `json:"message"`
}

// StatisticalBreachInput maps one outbox payload to an in-app notification for
// the rule owner. It is pure so the wiring stays unit-testable: the tenant and
// owner are validated here rather than silently notifying nobody.
func StatisticalBreachInput(payload []byte) (service.AcceptInput, error) {
	var event StatisticalBreachEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return service.AcceptInput{}, err
	}
	tenantID := strings.TrimSpace(event.TenantID)
	if tenantID == "" {
		return service.AcceptInput{}, errors.New("tenant_id is required")
	}
	owner := strings.TrimSpace(event.OwnerUserID)
	if owner == "" {
		return service.AcceptInput{}, errors.New("owner_user_id is required")
	}
	eventID := strings.TrimSpace(event.AlertID)
	if eventID == "" {
		eventID = strings.Join([]string{
			tenantID, event.RuleCode, event.PeriodCode, event.DimensionKey,
		}, "|")
	}
	ruleName := strings.TrimSpace(event.RuleName)
	if ruleName == "" {
		ruleName = event.RuleCode
	}
	params := map[string]any{
		"ruleCode":      event.RuleCode,
		"ruleName":      ruleName,
		"indicatorCode": event.IndicatorCode,
		"periodCode":    event.PeriodCode,
		"operator":      event.Operator,
		"threshold":     event.Threshold,
		"severity":      event.Severity,
		"message":       event.Message,
	}
	if event.Value != nil {
		params["value"] = *event.Value
	}
	return service.AcceptInput{
		TenantID:       tenantID,
		IdempotencyKey: "statistical.indicator.breached:" + eventID,
		SourceService:  "statistical-service",
		SourceEventID:  eventID,
		EventType:      "statistical.indicator.breached",
		TemplateKey:    "statistical.indicator.breached",
		Channels:       []string{domain.ChannelInApp},
		Recipients:     []domain.Recipient{{Type: "user", UserID: owner}},
		Payload:        params,
		Params:         params,
		Type:           breachNotificationType(event.Severity),
		Priority:       breachPriority(event.Severity),
	}, nil
}

func breachNotificationType(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high", "error":
		return "warning"
	default:
		return "info"
	}
}

func breachPriority(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 2
	case "high":
		return 1
	default:
		return 0
	}
}
