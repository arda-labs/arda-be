package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompleteSendsImagesThenPrompt(t *testing.T) {
	var got map[string]any
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		headers = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Preset: PresetOpenAICompatible, BaseURL: srv.URL + "/v1/", Model: "m1", APIKey: "secret"}, srv.Client())
	text, err := c.Complete(context.Background(), [][]byte{{0xFF, 0xD8, 1}, {0xFF, 0xD8, 2}}, "đọc thẻ")
	if err != nil || text != `{"ok":true}` {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if headers.Get("Authorization") != "Bearer secret" || headers.Get("x-opencode-session") != "" {
		t.Fatalf("headers = %v", headers)
	}
	if got["model"] != "m1" || got["temperature"] != float64(0) || got["stream"] != false {
		t.Fatalf("body = %v", got)
	}
	if _, has := got["response_format"]; has {
		t.Fatal("response_format must not be sent")
	}
	parts := got["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 3 {
		t.Fatalf("parts = %d", len(parts))
	}
	for i := 0; i < 2; i++ {
		p := parts[i].(map[string]any)
		url := p["image_url"].(map[string]any)["url"].(string)
		if p["type"] != "image_url" || !strings.HasPrefix(url, "data:image/jpeg;base64,") {
			t.Fatalf("part %d = %v", i, p)
		}
	}
	if last := parts[2].(map[string]any); last["type"] != "text" || last["text"] != "đọc thẻ" {
		t.Fatalf("last part = %v", last)
	}
}

func TestOpenCodeGoPresetHeaders(t *testing.T) {
	var sessions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessions = append(sessions, r.Header.Get("x-opencode-session"))
		if r.Header.Get("User-Agent") != "arda-document-service/test" {
			t.Errorf("ua = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{Preset: PresetOpenCodeGo, BaseURL: srv.URL, Model: "m", APIKey: "k", UserAgent: "arda-document-service/test"}, srv.Client())
	for i := 0; i < 2; i++ {
		if _, err := c.Complete(context.Background(), [][]byte{{1}}, "p"); err != nil {
			t.Fatal(err)
		}
	}
	if sessions[0] == "" || sessions[0] == sessions[1] {
		t.Fatalf("sessions = %v", sessions)
	}
}

func TestCompleteErrors(t *testing.T) {
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == 0 {
			time.Sleep(200 * time.Millisecond)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{Preset: PresetOpenAICompatible, BaseURL: srv.URL, Model: "m"}, srv.Client())

	if _, err := c.Complete(context.Background(), nil, "p"); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("500: %v", err)
	}
	status = http.StatusTooManyRequests
	if _, err := c.Complete(context.Background(), nil, "p"); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("429: %v", err)
	}
	status = 0
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Complete(ctx, nil, "p"); !errors.Is(err, ErrModelTimeout) {
		t.Fatalf("slow: %v", err)
	}
	if _, err := NewClient(ClientConfig{BaseURL: "http://127.0.0.1:1", Model: "m"}, nil).Complete(context.Background(), nil, "p"); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("refused: %v", err)
	}
}

func TestCompleteEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":null}}]}`))
	}))
	defer srv.Close()
	text, err := NewClient(ClientConfig{BaseURL: srv.URL, Model: "m"}, srv.Client()).Complete(context.Background(), nil, "p")
	if err != nil || text != "" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}
