package handler

import (
	"context"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

func TestFastPathTrivialQuery(t *testing.T) {
	trivialCases := []string{
		"xin chào",
		"chào bạn",
		"chào em",
		"chào bot",
		"hello",
		"hi",
		"cảm ơn",
		"cảm ơn bạn",
		"thank you",
		"tạm biệt",
		"bye",
		"ok",
		"oke",
		"vâng",
		"  xin chào!  ",
		"",
	}
	for _, tc := range trivialCases {
		msgs := []model.Message{{Role: "user", Content: tc}}
		if !isFastPathTrivialQuery(msgs) {
			t.Fatalf("expected trivial for %q", tc)
		}
	}

	nonTrivialCases := []string{
		"Phân tích dư nợ theo nhóm nợ kỳ 2026-08",
		"Chính sách cho vay khách hàng cá nhân",
		"Tạo tài khoản mới cho khách hàng",
		"chào bạn, hãy phân tích dư nợ kỳ này",
		"ok, hãy báo cáo số dư tiền gửi",
	}
	for _, tc := range nonTrivialCases {
		msgs := []model.Message{{Role: "user", Content: tc}}
		if isFastPathTrivialQuery(msgs) {
			t.Fatalf("expected non-trivial for %q", tc)
		}
	}
}

func TestSkillPacks(t *testing.T) {
	for _, skill := range []string{"loan_portfolio", "report", "knowledge"} {
		pack := getSkillPack(skill)
		if pack.ID != skill {
			t.Fatalf("pack ID = %q, want %q", pack.ID, skill)
		}
		if pack.Instructions == "" {
			t.Fatalf("pack %q instructions must not be empty", skill)
		}
		if len(pack.AllowedTools) == 0 {
			t.Fatalf("pack %q allowed tools must not be empty", skill)
		}
	}

	general := getSkillPack("general")
	if general.ID != "general" || general.AllowedTools != nil {
		t.Fatalf("general pack unexpected: %+v", general)
	}
}

func TestPruneToolDefinitionsDirectMode(t *testing.T) {
	allDefs := []model.ToolDef{
		{Name: "arda.statistical.getReportPresentation"},
		{Name: "arda.statistical.listReportDefinitions"},
		{Name: "arda.knowledge.search"},
		{Name: "arda.crm.getCustomer"},
		{Name: "arda.iam.listUsers"},
	}

	loanAllowed := skillAllowedTools("loan_portfolio")
	prunedLoan := pruneToolDefinitions(allDefs, loanAllowed)
	if len(prunedLoan) != 1 || prunedLoan[0].Name != "arda.statistical.getReportPresentation" {
		t.Fatalf("prunedLoan = %+v", prunedLoan)
	}

	knowledgeAllowed := skillAllowedTools("knowledge")
	prunedKnowledge := pruneToolDefinitions(allDefs, knowledgeAllowed)
	if len(prunedKnowledge) != 1 || prunedKnowledge[0].Name != "arda.knowledge.search" {
		t.Fatalf("prunedKnowledge = %+v", prunedKnowledge)
	}

	// Unknown or empty allowed returns all
	if len(pruneToolDefinitions(allDefs, nil)) != len(allDefs) {
		t.Fatal("nil allowed must keep all defs")
	}
}

func TestPruneToolDefinitionsPreservesCodeMode(t *testing.T) {
	codeModeDefs := []model.ToolDef{
		{Name: "search"},
		{Name: "execute"},
		{Name: "readResult"},
		{Name: "render_chart"},
	}

	loanAllowed := skillAllowedTools("loan_portfolio")
	pruned := pruneToolDefinitions(codeModeDefs, loanAllowed)
	if len(pruned) != len(codeModeDefs) {
		t.Fatalf("code mode defs must be preserved, got %d want %d", len(pruned), len(codeModeDefs))
	}
}

func TestRouteDecisionFastPathSkipsProvider(t *testing.T) {
	ctx := context.Background()
	run := repository.RunContext{TenantID: "tenant-1", ExternalRun: "run-1"}
	messages := []model.Message{{Role: "user", Content: "xin chào bạn"}}

	skill, out := routeDecision(ctx, nil, RouterOptions{}, run, messages)
	if skill != "" || len(out) != len(messages) {
		t.Fatalf("fast-path must return empty skill: skill=%q len=%d", skill, len(out))
	}
}
