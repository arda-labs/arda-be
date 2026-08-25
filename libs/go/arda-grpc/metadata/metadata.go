package metadata

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

const (
	RequestID      = "x-request-id"
	TraceID        = "x-trace-id"
	TraceParent    = "traceparent"
	TenantID       = "x-tenant-id"
	UserID         = "x-user-id"
	ActorUserID    = "x-actor-user-id"
	TargetUserID   = "x-target-user-id"
	UserSubject    = "x-user-subject"
	OrgID          = "x-org-id"
	OrgIDs         = "x-user-org-ids"
	GroupIDs       = "x-user-group-ids"
	Roles          = "x-roles"
	Permissions    = "x-permissions"
	AuthRisk       = "x-auth-risk"
	AuthChecked    = "x-auth-checked"
	SourceService  = "x-source-service"
	Locale         = "x-locale"
	IdempotencyKey = "x-idempotency-key"
	ServiceAccount = "x-service-account"
)

type Context struct {
	RequestID      string
	TraceID        string
	TraceParent    string
	TenantID       string
	UserID         string
	ActorUserID    string
	TargetUserID   string
	UserSubject    string
	OrgID          string
	OrgIDs         []string
	GroupIDs       []string
	Roles          []string
	Permissions    []string
	AuthRisk       string
	AuthChecked    string
	SourceService  string
	Locale         string
	IdempotencyKey string
	ServiceAccount string
}

func FromIncoming(ctx context.Context) Context {
	md, _ := metadata.FromIncomingContext(ctx)
	return fromMD(md)
}

func FromOutgoing(ctx context.Context) Context {
	md, _ := metadata.FromOutgoingContext(ctx)
	return fromMD(md)
}

// FromHTTPHeaders extracts the trusted BFF context so an HTTP handler can
// forward the same request context to downstream gRPC calls. It intentionally
// keeps actor and target separate; X-User-Id is the authenticated actor.
func FromHTTPHeaders(headers http.Header) Context {
	requestID := strings.TrimSpace(headers.Get("X-Request-Id"))
	if requestID == "" {
		requestID = uuid.NewString()
	}
	return Context{
		RequestID:      requestID,
		TraceID:        strings.TrimSpace(headers.Get("X-Trace-Id")),
		TraceParent:    strings.TrimSpace(headers.Get("traceparent")),
		TenantID:       strings.TrimSpace(headers.Get("X-Tenant-Id")),
		UserID:         strings.TrimSpace(headers.Get("X-User-Id")),
		ActorUserID:    strings.TrimSpace(headers.Get("X-Actor-User-Id")),
		TargetUserID:   strings.TrimSpace(headers.Get("X-Target-User-Id")),
		UserSubject:    strings.TrimSpace(headers.Get("X-User-Subject")),
		OrgID:          strings.TrimSpace(headers.Get("X-Org-Id")),
		OrgIDs:         split(headers.Get("X-User-Org-Ids")),
		GroupIDs:       split(headers.Get("X-User-Group-Ids")),
		Roles:          split(headers.Get("X-Roles")),
		Permissions:    split(headers.Get("X-Permissions")),
		AuthRisk:       strings.TrimSpace(headers.Get("X-Auth-Risk")),
		AuthChecked:    strings.TrimSpace(headers.Get("X-Auth-Checked")),
		Locale:         strings.TrimSpace(headers.Get("Accept-Language")),
		IdempotencyKey: strings.TrimSpace(headers.Get("Idempotency-Key")),
	}
}

// HTTPMiddleware binds BFF authentication and correlation headers to the
// request context used by downstream gRPC clients.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := AppendToOutgoing(r.Context(), FromHTTPHeaders(r.Header))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AppendToOutgoing(ctx context.Context, m Context) context.Context {
	pairs := []string{}
	add := func(key, value string) {
		if value != "" {
			pairs = append(pairs, key, value)
		}
	}
	add(RequestID, m.RequestID)
	add(TraceID, m.TraceID)
	add(TraceParent, m.TraceParent)
	add(TenantID, m.TenantID)
	add(UserID, m.UserID)
	add(ActorUserID, m.ActorUserID)
	add(TargetUserID, m.TargetUserID)
	add(UserSubject, m.UserSubject)
	add(OrgID, m.OrgID)
	add(OrgIDs, strings.Join(m.OrgIDs, ","))
	add(GroupIDs, strings.Join(m.GroupIDs, ","))
	add(Roles, strings.Join(m.Roles, ","))
	add(Permissions, strings.Join(m.Permissions, ","))
	add(AuthRisk, m.AuthRisk)
	add(AuthChecked, m.AuthChecked)
	add(SourceService, m.SourceService)
	add(Locale, m.Locale)
	add(IdempotencyKey, m.IdempotencyKey)
	add(ServiceAccount, m.ServiceAccount)
	if len(pairs) == 0 {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func fromMD(md metadata.MD) Context {
	return Context{
		RequestID:      first(md, RequestID),
		TraceID:        first(md, TraceID),
		TraceParent:    first(md, TraceParent),
		TenantID:       first(md, TenantID),
		UserID:         first(md, UserID),
		ActorUserID:    first(md, ActorUserID),
		TargetUserID:   first(md, TargetUserID),
		UserSubject:    first(md, UserSubject),
		OrgID:          first(md, OrgID),
		OrgIDs:         split(first(md, OrgIDs)),
		GroupIDs:       split(first(md, GroupIDs)),
		Roles:          split(first(md, Roles)),
		Permissions:    split(first(md, Permissions)),
		AuthRisk:       first(md, AuthRisk),
		AuthChecked:    first(md, AuthChecked),
		SourceService:  first(md, SourceService),
		Locale:         first(md, Locale),
		IdempotencyKey: first(md, IdempotencyKey),
		ServiceAccount: first(md, ServiceAccount),
	}
}

func first(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func split(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
