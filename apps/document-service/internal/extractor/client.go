package extractor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

var (
	ErrModelUnavailable = errors.New("model unavailable")
	ErrModelTimeout     = errors.New("model timeout")
)

type ClientConfig struct {
	Preset    string
	BaseURL   string
	Model     string
	APIKey    string
	UserAgent string
	MaxTokens int
}

// Client calls POST {BaseURL}/chat/completions. Both OpenCode Go (dev) and
// llama.cpp / vLLM (local) speak this format, so swapping models is config.
type Client struct {
	cfg  ClientConfig
	http *http.Client
}

func NewClient(cfg ClientConfig, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1500
	}
	return &Client{cfg: cfg, http: hc}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) Complete(ctx context.Context, images [][]byte, prompt string) (string, error) {
	parts := make([]contentPart, 0, len(images)+1)
	for _, img := range images {
		parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{
			URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img),
		}})
	}
	parts = append(parts, contentPart{Type: "text", Text: prompt})
	body, err := json.Marshal(chatRequest{
		Model:     c.cfg.Model,
		Messages:  []chatMessage{{Role: "user", Content: parts}},
		MaxTokens: c.cfg.MaxTokens,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	if c.cfg.Preset == PresetOpenCodeGo {
		req.Header.Set("x-opencode-session", newSessionID())
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if isTimeout(ctx, err) {
			return "", ErrModelTimeout
		}
		return "", fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if isTimeout(ctx, err) {
			return "", ErrModelTimeout
		}
		return "", fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d", ErrModelUnavailable, resp.StatusCode)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 || out.Choices[0].Message.Content == nil {
		return "", nil
	}
	return *out.Choices[0].Message.Content, nil
}

func isTimeout(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// newSessionID is random per request and carries no user data.
func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
