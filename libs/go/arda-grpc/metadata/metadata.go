package metadata

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	RequestID      = "x-request-id"
	TenantID       = "x-tenant-id"
	UserID         = "x-user-id"
	UserSubject    = "x-user-subject"
	Roles          = "x-roles"
	Permissions    = "x-permissions"
	SourceService  = "x-source-service"
	Locale         = "x-locale"
	IdempotencyKey = "x-idempotency-key"
	ServiceAccount = "x-service-account"
)

type Context struct {
	RequestID      string
	TenantID       string
	UserID         string
	UserSubject    string
	Roles          []string
	Permissions    []string
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

func AppendToOutgoing(ctx context.Context, m Context) context.Context {
	pairs := []string{}
	add := func(key, value string) {
		if value != "" {
			pairs = append(pairs, key, value)
		}
	}
	add(RequestID, m.RequestID)
	add(TenantID, m.TenantID)
	add(UserID, m.UserID)
	add(UserSubject, m.UserSubject)
	add(Roles, strings.Join(m.Roles, ","))
	add(Permissions, strings.Join(m.Permissions, ","))
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
		TenantID:       first(md, TenantID),
		UserID:         first(md, UserID),
		UserSubject:    first(md, UserSubject),
		Roles:          split(first(md, Roles)),
		Permissions:    split(first(md, Permissions)),
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
