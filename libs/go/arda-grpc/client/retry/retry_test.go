package retry

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const testService = "arda.finance.v1.PostingService"

func TestReadOnlyServiceConfigIsAcceptedByGRPC(t *testing.T) {
	conn, err := grpc.NewClient(
		"passthrough:///unused",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		ReadOnly(testService, "GetJournalEntry", "ListPostingRules"),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient rejected retry service config: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
}

func TestReadOnlyServiceConfigShape(t *testing.T) {
	raw := readOnlyServiceConfig(testService, []string{"GetJournalEntry"})
	var cfg struct {
		MethodConfig []struct {
			Name         []struct{ Service, Method string } `json:"name"`
			WaitForReady bool                               `json:"waitForReady"`
			RetryPolicy  struct {
				MaxAttempts          int      `json:"maxAttempts"`
				InitialBackoff       string   `json:"initialBackoff"`
				MaxBackoff           string   `json:"maxBackoff"`
				BackoffMultiplier    float64  `json:"backoffMultiplier"`
				RetryableStatusCodes []string `json:"retryableStatusCodes"`
			} `json:"retryPolicy"`
		} `json:"methodConfig"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal service config %q: %v", raw, err)
	}
	if len(cfg.MethodConfig) != 1 || len(cfg.MethodConfig[0].Name) != 1 {
		t.Fatalf("unexpected method config: %s", raw)
	}
	mc := cfg.MethodConfig[0]
	if mc.Name[0].Service != testService || mc.Name[0].Method != "GetJournalEntry" {
		t.Fatalf("unexpected method name: %+v", mc.Name[0])
	}
	if mc.WaitForReady {
		t.Fatal("waitForReady must stay false so calls fail fast")
	}
	if mc.RetryPolicy.MaxAttempts != 3 {
		t.Fatalf("maxAttempts = %d, want 3", mc.RetryPolicy.MaxAttempts)
	}
	if len(mc.RetryPolicy.RetryableStatusCodes) != 2 {
		t.Fatalf("retryableStatusCodes = %v, want UNAVAILABLE and RESOURCE_EXHAUSTED", mc.RetryPolicy.RetryableStatusCodes)
	}
}

func TestReadOnlyRetriesUnavailable(t *testing.T) {
	conn, attempts := startTestServer(t, []string{"GetJournalEntry"}, func(_ string, attempt int32) error {
		if attempt == 1 {
			return status.Error(codes.Unavailable, "connection reset")
		}
		return nil
	})

	invoke(t, conn, "GetJournalEntry", codes.OK)
	if got := attempts("GetJournalEntry"); got != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry)", got)
	}
}

func TestNonRetryableStatusIsNotRetried(t *testing.T) {
	conn, attempts := startTestServer(t, []string{"GetJournalEntry"}, func(string, int32) error {
		return status.Error(codes.InvalidArgument, "bad request")
	})

	invoke(t, conn, "GetJournalEntry", codes.InvalidArgument)
	if got := attempts("GetJournalEntry"); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry for InvalidArgument)", got)
	}
}

func TestUnlistedMutationIsNotRetried(t *testing.T) {
	conn, attempts := startTestServer(t, []string{"GetJournalEntry"}, func(string, int32) error {
		return status.Error(codes.Unavailable, "connection reset")
	})

	invoke(t, conn, "PostTransaction", codes.Unavailable)
	if got := attempts("PostTransaction"); got != 1 {
		t.Fatalf("attempts = %d, want 1 (mutations must never be replayed)", got)
	}
}

func invoke(t *testing.T, conn *grpc.ClientConn, method string, wantCode codes.Code) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := conn.Invoke(ctx, "/"+testService+"/"+method, &emptypb.Empty{}, &emptypb.Empty{})
	if wantCode == codes.OK {
		if err != nil {
			t.Fatalf("invoke %s: %v", method, err)
		}
		return
	}
	if got := status.Code(err); got != wantCode {
		t.Fatalf("invoke %s: code = %v, want %v (err: %v)", method, got, wantCode, err)
	}
}

// startTestServer runs a bufconn-backed server whose handlers report the
// attempt number to handle. The returned connection only retries
// retryReadMethods.
func startTestServer(t *testing.T, retryReadMethods []string, handle func(method string, attempt int32) error) (*grpc.ClientConn, func(method string) int32) {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()

	var mu sync.Mutex
	counters := map[string]int32{}
	dispatch := func(method string) func(any, context.Context, func(any) error, grpc.UnaryServerInterceptor) (any, error) {
		return func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			if err := dec(new(emptypb.Empty)); err != nil {
				return nil, err
			}
			mu.Lock()
			counters[method]++
			attempt := counters[method]
			mu.Unlock()
			if err := handle(method, attempt); err != nil {
				return nil, err
			}
			return &emptypb.Empty{}, nil
		}
	}

	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: testService,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "GetJournalEntry", Handler: dispatch("GetJournalEntry")},
			{MethodName: "PostTransaction", Handler: dispatch("PostTransaction")},
		},
	}, struct{}{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		ReadOnly(testService, retryReadMethods...),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	attempts := func(method string) int32 {
		mu.Lock()
		defer mu.Unlock()
		return counters[method]
	}
	return conn, attempts
}
