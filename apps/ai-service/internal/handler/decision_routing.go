package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

// Skills are reviewed, server-owned guidance. Classification never grants tools,
// chooses authorization, changes Act mode, or supplies executable code.
const reportSkill = `Report analysis skill:
- Resolve the requested period from the user's words and conversation. If missing or ambiguous, ask once and end this turn WITHOUT calling tools. Never infer a reporting period from calendarStatus and never probe different months to find data.
- Prefer statistical report definitions and getReportPresentation. listReportDefinitions limit must be <= 20; use a specific search instead of listing all reports.
- An empty report is a valid no-data result: explain which period has no data and stop. Do not switch to loan contracts or indicators to fabricate an equivalent report.
- Preserve returned period, units and actual snapshot date. Never label an older snapshot as current data.
- When the requested report is available, render its chart if present, summarize the verified data and finish. Optional label lookups or extra periods must not delay the answer. Only compare periods explicitly requested by the user.
- Compute arithmetic in code; do not invent thresholds or regulatory violations.`

func skillInstructions(skill string) string {
	switch skill {
	case "loan_portfolio":
		return reportSkill + "\nThe matching report is LOAN_PORTFOLIO (dư nợ theo nhóm nợ). Use arda.statistical.getReportPresentation with this code and the user-confirmed period; no report catalog discovery is needed."
	case "report":
		return reportSkill
	case "knowledge":
		return "Knowledge lookup skill: use arda.knowledge.search for documented policies and instructions. Answer from relevant returned evidence with citations. If no relevant evidence exists, explain that once and stop. Do not browse unrelated domains to replace missing evidence."
	default:
		return ""
	}
}

func routeDecision(ctx context.Context, store runStore, options RouterOptions, run repository.RunContext, messages []model.Message) (string, []model.Message) {
	settingsStore, ok := store.(repository.DecisionSettingsStore)
	if !ok {
		return "", messages
	}
	// One bounded classification per new run; never classify approval resumes.
	ctx, cancel := context.WithTimeout(ctx, decision.Timeout)
	defer cancel()
	settings, err := settingsStore.GetDecisionSettings(ctx, run.TenantID)
	if err != nil {
		slog.Warn("decision routing skipped", "reason", "settings_unavailable", "run_id", run.ExternalRun)
		return "", messages
	}
	if !settings.Enabled {
		return "", messages
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, decision.BaseURL) {
		return "", messages
	}
	// No identity, raw report data, tool outputs or secrets go to the router.
	var userMessages []string
	for _, message := range messages {
		if message.Role == "user" {
			userMessages = append(userMessages, message.Content)
		}
	}
	if len(userMessages) == 0 {
		return "", messages
	}
	latest := userMessages[len(userMessages)-1]
	if len(latest) > 4096 {
		return "", messages
	} // never classify truncated intent
	state := "Latest user request: " + sanitizeTranscript(latest)
	if len(userMessages) > 1 {
		previous := userMessages[len(userMessages)-2]
		if len(previous) <= 2048 {
			state = "Previous user request (context only): " + sanitizeTranscript(previous) + "\n" + state
		}
	}
	start := time.Now()
	result, err := decisionClient(options).Evaluate(ctx, settings, state)
	if err != nil {
		slog.Warn("decision routing skipped", "reason", "provider_unavailable", "run_id", run.ExternalRun)
		return "", messages
	}
	skill := result.Skill(settings.MinConfidence)
	if recorder, ok := store.(repository.DecisionRecorder); ok {
		persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		err = recorder.RecordDecision(persistCtx, run, result, skill)
		cancelPersist()
		if err != nil {
			slog.Warn("decision usage persistence failed", "run_id", run.ExternalRun)
		}
	}
	slog.Info("decision routing evaluated", "run_id", run.ExternalRun, "skill", skill,
		"model_id", result.Model, "latency_ms", time.Since(start).Milliseconds(), "confidence", decisionConfidence(result))
	if instructions := skillInstructions(skill); instructions != "" {
		out := append([]model.Message{}, messages[:len(messages)-1]...)
		out = append(out, model.Message{Role: "system", Content: instructions}, messages[len(messages)-1])
		return skill, out
	}
	return "", messages
}
