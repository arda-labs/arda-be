package evaluation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
)

type fakeQuestionsEvaluator struct {
	state     string
	questions map[string]decision.Question
	result    *decision.Result
	err       error
}

func (f *fakeQuestionsEvaluator) EvaluateQuestions(_ context.Context, _ decision.Settings, state string, questions map[string]decision.Question) (*decision.Result, error) {
	f.state, f.questions = state, questions
	return f.result, f.err
}

func judgeSettings() decision.Settings {
	return decision.Settings{Enabled: true, ModelID: decision.DefaultModel, MinConfidence: 0.8, APIKey: "key"}
}

func TestJevJudgeBatchesServerOwnedQuestions(t *testing.T) {
	evaluator := &fakeQuestionsEvaluator{result: &decision.Result{Answers: map[string]decision.Answer{
		"grounded":         decision.NoulAnswer(0.9),
		"answers_question": decision.NoulAnswer(0.8),
	}}}
	judge := JevJudge(evaluator, judgeSettings())
	out := judge(context.Background(), AnswerCase{ID: "c1", Query: "Dư nợ nhóm 3-5 là bao nhiêu?"},
		AnswerResponse{Text: "Dư nợ nhóm 3-5 là 12 tỷ [7:heading]", ToolContents: []string{"chunk [7:heading] nội dung"}})
	if out.Error != "" {
		t.Fatalf("judge error: %s", out.Error)
	}
	if out.Grounded == nil || *out.Grounded != 0.9 || out.AnswersQuestion == nil || *out.AnswersQuestion != 0.8 {
		t.Fatalf("unexpected judge result: %+v", out)
	}
	if _, ok := evaluator.questions["correct_abstention"]; ok {
		t.Fatal("abstention question must only be asked for no-answer cases")
	}
	if !strings.Contains(evaluator.state, "Question: Dư nợ nhóm 3-5 là bao nhiêu?") ||
		!strings.Contains(evaluator.state, "Evidence:") ||
		!strings.Contains(evaluator.state, "[7:heading]") {
		t.Fatalf("unexpected judge state: %q", evaluator.state)
	}
	if !strings.Contains(evaluator.questions["grounded"].Instructions, "untrusted data") {
		t.Fatal("grounded question must frame the state as untrusted data")
	}
}

func TestJevJudgeAddsAbstentionForNoAnswerCases(t *testing.T) {
	evaluator := &fakeQuestionsEvaluator{result: &decision.Result{Answers: map[string]decision.Answer{
		"grounded":           decision.NoulAnswer(0.6),
		"answers_question":   decision.NoulAnswer(0.7),
		"correct_abstention": decision.NoulAnswer(0.95),
	}}}
	judge := JevJudge(evaluator, judgeSettings())
	out := judge(context.Background(), AnswerCase{ID: "c2", Query: "Giá vàng hôm nay?", Expected: AnswerExpected{AllowNoAnswer: true}},
		AnswerResponse{Text: "Tôi chưa có dữ liệu về giá vàng."})
	if out.CorrectAbstention == nil || *out.CorrectAbstention != 0.95 {
		t.Fatalf("unexpected abstention: %+v", out)
	}
	if _, ok := evaluator.questions["correct_abstention"]; !ok {
		t.Fatal("abstention question missing for no-answer case")
	}
}

func TestJevJudgeReportsErrorsWithoutValues(t *testing.T) {
	evaluator := &fakeQuestionsEvaluator{err: errors.New("provider unavailable")}
	judge := JevJudge(evaluator, judgeSettings())
	out := judge(context.Background(), AnswerCase{ID: "c3", Query: "?"}, AnswerResponse{Text: "answer"})
	if out.Error == "" || out.Grounded != nil || out.AnswersQuestion != nil {
		t.Fatalf("unexpected failing judge result: %+v", out)
	}
}

func TestJudgeStateBoundsEvidence(t *testing.T) {
	state := judgeState(
		AnswerCase{Query: strings.Repeat("q", 2000)},
		AnswerResponse{Text: strings.Repeat("a", 9000), ToolContents: []string{strings.Repeat("e", 20000)}},
	)
	if len(state) > maxJudgeStateBytes {
		t.Fatalf("state = %d bytes, want <= %d", len(state), maxJudgeStateBytes)
	}
}

func TestSelectEvidenceExtractsChunksAndFallsBack(t *testing.T) {
	object := `{"data":{"output":[{"content":"Nghỉ phép năm: 12 ngày làm việc."}]}}`
	evidence := selectEvidence([]string{object})
	if !strings.Contains(evidence, "12 ngày làm việc") {
		t.Fatalf("object chunk missing: %q", evidence)
	}

	pairs := `{"data":{"output":[[["heading","Phép năm"],["content","Mọi nhân viên hợp đồng chính thức được 12 ngày phép năm."]]]}}`
	if !strings.Contains(selectEvidence([]string{pairs}), "12 ngày phép năm") {
		t.Fatalf("pair-shaped chunk missing: %q", selectEvidence([]string{pairs}))
	}

	duplicated := selectEvidence([]string{object, object})
	if strings.Count(duplicated, "Nghỉ phép năm: 12 ngày làm việc.") != 1 {
		t.Fatalf("duplicate chunk not deduplicated: %q", duplicated)
	}

	citationOnly := strings.Repeat("x", 100) + " [12:quy trình] " + strings.Repeat("y", 5000)
	citationEvidence := selectEvidence([]string{citationOnly})
	if !strings.Contains(citationEvidence, "[12:quy trình]") {
		t.Fatalf("citation fallback missing: %q", citationEvidence)
	}
	if len(citationEvidence) > judgeEvidenceLimit {
		t.Fatalf("evidence = %d bytes, want <= %d", len(citationEvidence), judgeEvidenceLimit)
	}

	head := selectEvidence([]string{strings.Repeat("z", 9000)})
	if len(head) != judgeSnippetLimit {
		t.Fatalf("fallback = %d bytes, want %d", len(head), judgeSnippetLimit)
	}
	if snippet := snippetAround("abc", 2); snippet != "abc" {
		t.Fatalf("small snippet = %q", snippet)
	}
}
