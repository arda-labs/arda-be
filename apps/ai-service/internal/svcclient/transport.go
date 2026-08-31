package svcclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

const (
	defaultTimeout     = 5 * time.Second
	defaultMaxResponse = 512 << 10 // 512 KB
	maxTokenTTL        = 5 * time.Minute
)

// Client is a service-to-service HTTP client. It authenticates the calling
// service (Source) to ServiceName with a signed x-service-auth assertion and
// forwards the delegated subject context (user/tenant/org) as headers. The
// caller identity is derived solely from Source + Secret and can never be
// overridden by request arguments.
type Client struct {
	ServiceName string
	BaseURL     string
	Source      string
	Secret      string
	HTTPClient  *http.Client
	Timeout     time.Duration
	MaxResponse int64
}

// NewClient returns a Client targeting serviceName at baseURL. The source is
// the caller identity presented to the target (e.g. "ai-service").
func NewClient(serviceName, baseURL, source, secret string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{
		ServiceName: serviceName,
		BaseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Source:      source,
		Secret:      secret,
		HTTPClient:  hc,
		Timeout:     defaultTimeout,
		MaxResponse: defaultMaxResponse,
	}
}

// NewRequest creates a signed request to the target service at path and
// attaches the delegated subject headers. `md` is the verified context of the
// user the caller acts for; it is never derived from tool arguments.
func (c *Client) NewRequest(ctx context.Context, method, path string, md metadata.Context) (*http.Request, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("%s base URL is not configured", c.ServiceName)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", c.ServiceName, err)
	}
	if err := identity.SignRequest(req, c.Secret, c.Source, c.ServiceName, time.Now(), maxTokenTTL); err != nil {
		return nil, fmt.Errorf("sign %s request: %w", c.ServiceName, err)
	}
	md.SourceService = c.Source
	metadata.AppendToHTTP(req.Header, md)
	return req, nil
}

// StatusError reports a non-2xx response from the target service.
type StatusError struct {
	Service string
	Status  int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned status %d", e.Service, e.Status)
}

// Do executes the request with a per-call timeout and a bounded response read
// (MaxResponse bytes; larger responses are rejected). Idempotent methods
// (GET/HEAD/OPTIONS) retry once on transient errors and 5xx responses;
// mutations are never auto-retried.
func (c *Client) Do(ctx context.Context, req *http.Request, out any) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(ctx)

	maxResponse := c.MaxResponse
	if maxResponse <= 0 {
		maxResponse = defaultMaxResponse
	}

	attempts := 1
	if isIdempotent(req.Method) {
		attempts = 2
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(50 * time.Millisecond): // minimal backoff before retry
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s request failed: %w", c.ServiceName, err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
			statusErr := &StatusError{Service: c.ServiceName, Status: resp.StatusCode}
			if resp.StatusCode >= 500 && attempts > 1 {
				lastErr = statusErr
				continue
			}
			return statusErr
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read %s response: %w", c.ServiceName, err)
		}
		if int64(len(body)) > maxResponse {
			return fmt.Errorf("%s response exceeds %d bytes", c.ServiceName, maxResponse)
		}
		if out != nil {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("decode %s response: %w", c.ServiceName, err)
			}
		}
		return nil
	}
	return lastErr
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
