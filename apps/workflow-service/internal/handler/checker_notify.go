package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/workflow-service/internal/notificationclient"
	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func reviewDecisionFromVariables(variables map[string]any) string {
	if variables == nil {
		return ""
	}
	if v, ok := variables["reviewDecision"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := variables["approvalResult"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func reviewCommentFromVariables(variables map[string]any) string {
	if variables == nil {
		return ""
	}
	if v, ok := variables["reviewComment"].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := variables["comment"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func requireReviewComment(decision, comment string) error {
	switch decision {
	case "REQUEST_CHANGES", "REJECT":
		if strings.TrimSpace(comment) == "" {
			return fmt.Errorf("reviewComment is required for %s", decision)
		}
	}
	return nil
}

func checkerTimelineEventType(decision string) string {
	switch decision {
	case "REQUEST_CHANGES":
		return "CHECKER_REQUEST_CHANGES"
	case "REJECT":
		return "CHECKER_REJECTED"
	case "APPROVE":
		return "CHECKER_APPROVED"
	default:
		return ""
	}
}

func (h *WorkflowHandler) recordCheckerDecisionTimeline(ctx context.Context, caseID, decision, comment, actor string) {
	eventType := checkerTimelineEventType(decision)
	if eventType == "" || h.caseRepo == nil || caseID == "" {
		return
	}
	note := comment
	if note == "" && decision == "APPROVE" {
		note = "Approved"
	}
	if err := h.caseRepo.AddTimelineEvent(ctx, caseID, eventType, note); err != nil {
		slog.Warn("failed to record checker decision timeline", "caseId", caseID, "eventType", eventType, "err", err)
	}
	_ = actor
}

func (h *WorkflowHandler) notifyCheckerDecision(ctx context.Context, bc *repository.BusinessCase, jobKey int64, decision, comment string) {
	if h.notificationClient == nil || !h.notificationClient.Enabled() || bc == nil {
		return
	}
	makerID := strings.TrimSpace(bc.CreatedBy)
	if makerID == "" {
		slog.Warn("skip checker decision notification: missing maker", "caseId", bc.ID, "decision", decision)
		return
	}
	templateKey, kind, titleKey, bodyKey := checkerNotificationKeys(decision)
	if templateKey == "" {
		return
	}
	customerID := strings.TrimSpace(bc.PrimaryObjectID)
	href := "/customers/registrations"
	if customerID != "" {
		q := url.Values{}
		q.Set("customerId", customerID)
		if bc.ID != "" {
			q.Set("caseId", bc.ID)
		}
		href = "/customers/registrations?" + q.Encode()
	}
	params := map[string]any{
		"caseCode": bc.CaseCode,
		"comment":  comment,
	}
	if customerID != "" {
		params["customerId"] = customerID
	}
	tenantID := strings.TrimSpace(bc.TenantID)
	if tenantID == "" {
		slog.Warn("skip checker decision notification: missing tenant", "caseId", bc.ID, "decision", decision)
		return
	}
	idempotencyKey := fmt.Sprintf("workflow:%s:%d:%s", bc.ID, jobKey, decision)
	err := h.notificationClient.Accept(ctx, notificationclient.AcceptRequest{
		TenantID:       tenantID,
		IdempotencyKey: idempotencyKey,
		SourceService:  "workflow-service",
		SourceEventID:  strconv.FormatInt(jobKey, 10),
		EventType:      "workflow.checker.decision",
		TemplateKey:    templateKey,
		Channels:       []string{"in_app"},
		Recipients: []notificationclient.Recipient{{
			Type:   "user",
			UserID: makerID,
		}},
		Payload: map[string]any{
			"caseId":   bc.ID,
			"decision": decision,
		},
		CorrelationID: bc.ID,
		Type:          kind,
		TitleKey:      titleKey,
		BodyKey:       bodyKey,
		Href:          href,
		Params:        params,
	})
	if err != nil {
		slog.Warn("checker decision notification failed",
			"caseId", bc.ID,
			"decision", decision,
			"jobKey", jobKey,
			"err", err,
		)
		return
	}
	slog.Info("checker decision notification accepted",
		"caseId", bc.ID,
		"decision", decision,
		"jobKey", jobKey,
		"makerId", makerID,
	)
}

func checkerNotificationKeys(decision string) (templateKey, kind, titleKey, bodyKey string) {
	switch decision {
	case "REQUEST_CHANGES":
		return "crm.customer.request_changes", "warning",
			"crm.customer.request_changes.title",
			"crm.customer.request_changes.body"
	case "REJECT":
		return "crm.customer.rejected", "error",
			"crm.customer.rejected.title",
			"crm.customer.rejected.body"
	case "APPROVE":
		return "crm.customer.approved", "success",
			"crm.customer.approved.title",
			"crm.customer.approved.body"
	default:
		return "", "", "", ""
	}
}
