package events

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
)

// Subjects platform-service publishes (via its outbox relay) when a scheduled
// job run fails terminally or succeeds again after a failure.
const (
	PlatformJobFailedSubject    = ardaevents.SubjectPlatformJobFailed
	PlatformJobRecoveredSubject = ardaevents.SubjectPlatformJobRecovered
)

// PlatformJobEvent mirrors the JSON of platform-service domain.JobEvent.
type PlatformJobEvent struct {
	EventID            string   `json:"event_id"`
	OccurredAt         string   `json:"occurred_at"`
	JobCode            string   `json:"job_code"`
	JobName            string   `json:"job_name"`
	RunID              string   `json:"run_id"`
	Status             string   `json:"status"`
	Attempt            int      `json:"attempt"`
	ErrorCode          string   `json:"error_code"`
	ErrorMessage       string   `json:"error_message"`
	TenantID           string   `json:"tenant_id"`
	UserIDs            []string `json:"user_ids"`
	GroupIDs           []string `json:"group_ids"`
	RoleCodes          []string `json:"role_codes"`
	OrgUnitIDs         []string `json:"org_unit_ids"`
	IncludeDescendants bool     `json:"include_descendants"`
	Href               string   `json:"href"`
	Locale             string   `json:"locale"`
}

// PlatformJobInput maps one job event to an in-app notification. Recipients
// are taken from user_ids, which the dispatcher has already resolved.
func PlatformJobInput(payload []byte, subject, defaultLocale string) (service.AcceptInput, error) {
	var event PlatformJobEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return service.AcceptInput{}, err
	}
	if strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(event.RunID) == "" || strings.TrimSpace(event.EventID) == "" {
		return service.AcceptInput{}, errors.New("platform job event requires tenant_id, run_id and event_id")
	}
	locale, err := ResolveLocale("", event.Locale, defaultLocale)
	if err != nil {
		return service.AcceptInput{}, err
	}
	eventType := strings.TrimPrefix(subject, "arda.")
	in := businessInput(event.TenantID, event.EventID, "platform-service", eventType, "scheduled_job_run", event.RunID,
		locale, event.Status, event.JobName, event.Href, event.UserIDs)
	// Params and Payload share one map in businessInput, so both carry these.
	in.Params["jobName"] = event.JobName
	in.Params["jobCode"] = event.JobCode
	in.Params["errorMessage"] = event.ErrorMessage
	if subject == PlatformJobFailedSubject {
		in.Type = "error"
	} else {
		in.Type = "success"
	}
	return in, nil
}
