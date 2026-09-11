package service

import (
	"context"
	"errors"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

// NotificationEvent is one entry in the notification event registry — the
// event codes the platform recognizes for notification templates (fe_common
// #17 / fe_bpm #7). HasTemplate is resolved against noti_templates.
type NotificationEvent struct {
	Code        string `json:"code"`
	Subject     string `json:"subject"`
	Domain      string `json:"domain"`
	Description string `json:"description"`
	HasTemplate bool   `json:"has_template"`
}

// notificationEventRegistry lists the platform notification events. Domain
// services emit the matching subjects on the event bus; operators configure
// content per event in noti_templates.
var notificationEventRegistry = []NotificationEvent{
	{Code: "loan.disbursement.completed", Subject: "arda.loan.disbursement.completed.v1", Domain: "LNM", Description: "Giải ngân khoản vay hoàn tất"},
	{Code: "loan.collection.created", Subject: "arda.loan.collection.created.v1", Domain: "LNM", Description: "Phát sinh thu nợ"},
	{Code: "loan.provision.posted", Subject: "arda.loan.provision.posted.v1", Domain: "LNM", Description: "Trích lập dự phòng"},
	{Code: "deposit.maturity.upcoming", Subject: "arda.deposit.maturity.upcoming.v1", Domain: "DPM", Description: "Sổ tiết kiệm sắp đáo hạn"},
	{Code: "interbank.movement.approved", Subject: "arda.interbank.movement.approved.v1", Domain: "IBM", Description: "Giao dịch liên ngân hàng được duyệt"},
	{Code: "capital.formation.approved", Subject: "arda.capital.formation.approved.v1", Domain: "CFM", Description: "Hợp đồng nguồn vốn được duyệt"},
	{Code: "finance.posting.approved", Subject: "arda.finance.posting.approved.v1", Domain: "FIN", Description: "Bút toán kế toán được duyệt"},
	{Code: "report.submission.approved", Subject: "arda.report.submission.approved.v1", Domain: "RPT", Description: "Báo cáo được duyệt"},
	{Code: "hrm.employee.registered", Subject: "arda.hrm.employee.registered.v1", Domain: "HRM", Description: "Nhân sự mới được đăng ký"},
	{Code: "workflow.task.assigned", Subject: "arda.workflow.task.assigned.v1", Domain: "BPM", Description: "Công việc được phân công"},
	{Code: "notification.inbox.created", Subject: "arda.notification.inbox.created.v1", Domain: "NOTI", Description: "Thông báo hộp thư đến"},
}

// ListEvents returns the event registry with a has_template flag per event.
func (s *NotificationService) ListEvents(ctx context.Context, tenantID string) ([]NotificationEvent, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	templates, err := s.repo.ListTemplates(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	hasTemplate := map[string]bool{}
	for _, t := range templates {
		hasTemplate[t.EventCode] = true
	}
	out := make([]NotificationEvent, 0, len(notificationEventRegistry))
	for _, e := range notificationEventRegistry {
		e.HasTemplate = hasTemplate[e.Code]
		out = append(out, e)
	}
	return out, nil
}

// ListOutboxDLQ returns pending dead-lettered events for the tenant.
func (s *NotificationService) ListOutboxDLQ(ctx context.Context, tenantID string, limit int) ([]repository.OutboxDLQEntry, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	return s.repo.ListOutboxDLQ(ctx, tenantID, limit)
}

// ReplayOutboxDLQ requeues one dead-lettered event (operator identity required).
func (s *NotificationService) ReplayOutboxDLQ(ctx context.Context, tenantID, outboxID, operator string) error {
	if tenantID == "" {
		return ErrTenantScopeRequired
	}
	if strings.TrimSpace(operator) == "" {
		return errors.New("operator identity is required")
	}
	return s.repo.ReplayOutboxDLQForTenant(ctx, tenantID, outboxID, operator)
}

// DiscardOutboxDLQ permanently deletes one dead-lettered event.
func (s *NotificationService) DiscardOutboxDLQ(ctx context.Context, tenantID, outboxID string) error {
	if tenantID == "" {
		return ErrTenantScopeRequired
	}
	return s.repo.DiscardOutboxDLQ(ctx, tenantID, outboxID)
}
