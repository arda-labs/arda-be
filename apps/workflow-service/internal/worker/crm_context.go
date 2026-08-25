package worker

import (
	"context"
	"strings"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
)

// crmJobContext carries the workflow case scope to CRM. A worker has no HTTP
// request, so the scope must come from versioned process variables; missing
// tenant/org intentionally fails closed in the CRM gRPC server.
func crmJobContext(job entities.Job) context.Context {
	vars, _ := job.GetVariablesAsMap()
	tenantID := stringVariable(vars, "tenantId", "tenant_id")
	orgID := stringVariable(vars, "orgId", "org_id")
	actorID := stringVariable(vars, "actorUserId", "actor_user_id", "createdBy", "created_by")
	return ardametadata.AppendToOutgoing(context.Background(), ardametadata.Context{
		TenantID:       tenantID,
		OrgID:          orgID,
		OrgIDs:         nonEmpty(orgID),
		ActorUserID:    actorID,
		ServiceAccount: "workflow-service",
	})
}

func stringVariable(vars map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := vars[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
