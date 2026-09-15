package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sseServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/agent" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-Auth-Checked") != "true" ||
			r.Header.Get("X-User-Id") != "eval-user" ||
			r.Header.Get("X-Tenant-Id") != "tenant-1" ||
			!strings.Contains(r.Header.Get("X-Permissions"), "ai.assistant.use") {
			t.Errorf("identity headers missing: %+v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, event := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

func TestRunAnswersScoresCitationsKeywordsAndNoAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Checked") != "true" ||
			r.Header.Get("X-User-Id") != "eval-user" ||
			r.Header.Get("X-Tenant-Id") != "tenant-1" ||
			!strings.Contains(r.Header.Get("X-Permissions"), "ai.assistant.use") {
			t.Errorf("identity headers missing: %+v", r.Header)
		}
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		query := ""
		if len(payload.Messages) > 0 {
			query = payload.Messages[0].Content
		}

		var events []string
		if strings.Contains(query, "Giá vàng") {
			events = []string{
				`{"type":"TEXT_MESSAGE_CONTENT","delta":"Tôi chưa có dữ liệu về giá vàng."}`,
				`{"type":"RUN_FINISHED","outcome":{"type":"success"}}`,
			}
		} else {
			events = []string{
				`{"type":"RUN_STARTED"}`,
				`{"type":"TEXT_MESSAGE_CONTENT","delta":"Olorin là trợ lý AI "}`,
				`{"type":"TOOL_CALL_RESULT","content":"{\"citation\":\"[12:Olorin là gì?]\",\"content\":\"...\"}"}`,
				`{"type":"TEXT_MESSAGE_CONTENT","delta":"của nền tảng Arda."}`,
				`{"type":"RUN_FINISHED","outcome":{"type":"success"}}`,
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, event := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	set := AnswerSet{Version: 1, Cases: []AnswerCase{
		{
			ID: "faq-what-is-olorin", Query: "Olorin là gì?",
			Expected: AnswerExpected{MustCite: true, Keywords: []string{"trợ lý AI"}},
		},
		{
			ID: "keyword-miss", Query: "Olorin là gì?",
			Expected: AnswerExpected{MustCite: true, Keywords: []string{"không bao giờ xuất hiện"}},
		},
		{
			ID: "out-of-corpus", Query: "Giá vàng hôm nay?",
			Expected: AnswerExpected{AllowNoAnswer: true},
		},
	}}

	ask := HTTPAsk(server.URL, "eval-user", "tenant-1", "ai.assistant.use,ai.knowledge.read", "", server.Client())
	report := RunAnswers(context.Background(), set, ask)

	if report.Passed != 2 || report.Failed != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Cases[0].CitationCount != 1 {
		t.Fatalf("citation count = %d, want 1", report.Cases[0].CitationCount)
	}
	if !strings.Contains(report.Cases[0].AnswerExcerpt, "trợ lý AI") {
		t.Fatalf("answer excerpt missing text: %q", report.Cases[0].AnswerExcerpt)
	}
	if len(report.Cases[1].MissingWords) != 1 {
		t.Fatalf("keyword miss not reported: %+v", report.Cases[1])
	}
	if !report.Cases[2].Passed || report.Cases[2].CitationCount != 0 {
		t.Fatalf("no-answer case = %+v", report.Cases[2])
	}
}

func TestRunAnswersTreatsInterruptAsFailure(t *testing.T) {
	server := sseServer(t, []string{
		`{"type":"TOOL_CALL_RESULT","result":{"proposal":{"id":"approval-1","status":"PENDING"}}}`,
		`{"type":"RUN_FINISHED","outcome":{"type":"interrupt"}}`,
	})
	defer server.Close()

	set := AnswerSet{Version: 1, Cases: []AnswerCase{{
		ID: "interrupt-case", Query: "Xuất dữ liệu khách C-7",
		Expected: AnswerExpected{},
	}}}
	ask := HTTPAsk(server.URL, "eval-user", "tenant-1", "ai.assistant.use", "", server.Client())
	report := RunAnswers(context.Background(), set, ask)

	if report.Failed != 1 {
		t.Fatalf("interrupt must fail the case: %+v", report)
	}
	if !strings.Contains(report.Cases[0].Error, "human approval") {
		t.Fatalf("error = %q, want approval interrupt", report.Cases[0].Error)
	}
}

func TestParseAnswerSetValidates(t *testing.T) {
	if _, err := ParseAnswerSet([]byte("version: 1\ncases: []\n")); err == nil {
		t.Fatal("empty case list must be rejected")
	}
	set, err := ParseAnswerSet([]byte("version: 1\ncases:\n  - id: a\n    query: hỏi\n    expected:\n      must_cite: true\n"))
	if err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}
	if !set.Cases[0].Expected.MustCite {
		t.Fatalf("case expectation not parsed: %+v", set.Cases[0])
	}
}
