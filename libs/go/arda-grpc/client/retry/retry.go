// Package retry holds the shared gRPC retry policy for the arda service
// clients under libs/go/arda-grpc/client.
//
// The policy is intentionally narrow:
//
//   - Only the read-only methods the caller names are retried. gRPC replays
//     the buffered request when a retryable status arrives, so retrying a
//     mutation could duplicate it (e.g. a second journal entry). Every RPC
//     that changes state must stay out of the list.
//   - Only UNAVAILABLE and RESOURCE_EXHAUSTED are retried: both are transient
//     transport/server-pressure signals. INVALID_ARGUMENT, PERMISSION_DENIED,
//     FAILED_PRECONDITION, NOT_FOUND, ... are never retried because a replay
//     cannot change their outcome.
//   - At most 3 attempts (initial + 2 retries) with a 50ms → 500ms
//     exponential backoff; gRPC adds 0.8-1.2 jitter. A brief pod restart or
//     connection reset heals without turning an outage into a retry storm.
//   - waitForReady stays false, so a call against an unreachable dependency
//     fails fast within the caller's own timeout instead of blocking. Retries
//     still cover attempts that reach the server and return a retryable
//     status, plus the transparent retries the channel performs before
//     anything is written to the wire.
package retry

import (
	"encoding/json"
	"strings"

	"google.golang.org/grpc"
)

const (
	// maxAttempts is the total number of RPC attempts (1 initial + 2 retries).
	// gRPC's default cap for service-config retries is 5; keeping it lower
	// bounds the worst-case latency contribution of the policy.
	maxAttempts = 3
	// initialBackoff/maxBackoff bound the exponential backoff between
	// attempts. Values are protobuf JSON durations.
	initialBackoff    = "0.05s"
	maxBackoff        = "0.5s"
	backoffMultiplier = 2.0
)

// retryableStatusCodes lists the only gRPC codes the shared policy replays.
var retryableStatusCodes = []string{"UNAVAILABLE", "RESOURCE_EXHAUSTED"}

type serviceConfig struct {
	MethodConfig []methodConfig `json:"methodConfig"`
}

type methodConfig struct {
	Name         []methodName `json:"name"`
	WaitForReady bool         `json:"waitForReady"`
	RetryPolicy  retryPolicy  `json:"retryPolicy"`
}

type methodName struct {
	Service string `json:"service"`
	Method  string `json:"method"`
}

type retryPolicy struct {
	MaxAttempts          int      `json:"maxAttempts"`
	InitialBackoff       string   `json:"initialBackoff"`
	MaxBackoff           string   `json:"maxBackoff"`
	BackoffMultiplier    float64  `json:"backoffMultiplier"`
	RetryableStatusCodes []string `json:"retryableStatusCodes"`
}

// ReadOnly returns a DialOption that enables the shared bounded retry policy
// for the named methods of service. Methods are protobuf method names without
// the leading slash (e.g. "GetJournalEntry"); service is the fully-qualified
// protobuf service name (e.g. "arda.finance.v1.PostingService").
//
// Only pass methods that are safe to replay: reads, dry-runs and idempotent
// lookups. Never list a mutation. When no method is given the option is a
// no-op service config, which keeps call sites uniform.
func ReadOnly(service string, methods ...string) grpc.DialOption {
	return grpc.WithDefaultServiceConfig(readOnlyServiceConfig(service, methods))
}

func readOnlyServiceConfig(service string, methods []string) string {
	service = strings.TrimSpace(service)
	names := make([]methodName, 0, len(methods))
	for _, method := range methods {
		if method = strings.TrimSpace(method); method == "" {
			continue
		}
		names = append(names, methodName{Service: service, Method: method})
	}
	if len(names) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(serviceConfig{MethodConfig: []methodConfig{{
		Name:         names,
		WaitForReady: false,
		RetryPolicy: retryPolicy{
			MaxAttempts:          maxAttempts,
			InitialBackoff:       initialBackoff,
			MaxBackoff:           maxBackoff,
			BackoffMultiplier:    backoffMultiplier,
			RetryableStatusCodes: retryableStatusCodes,
		},
	}}})
	if err != nil {
		// The struct shape is fixed and always JSON-marshalable; this cannot
		// happen. Panicking keeps the DialOption builder infallible.
		panic("arda-grpc/client/retry: marshal service config: " + err.Error())
	}
	return string(raw)
}
