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

// traderStampKeys are the fixed metadata keys the finance journal entry
// carries for the trader stamp (iteration 12 — hạch toán ai giao dịch, mirror
// of the trader block the FE sends on cancellation/manual-posting cases).
var traderStampKeys = map[string]string{
	"objectType": "trader_object_type",
	"objectCode": "trader_object_code",
	"objectName": "trader_object_name",
	"idNumber":   "trader_id_number",
	"issueDate":  "trader_issue_date",
	"issuePlace": "trader_issue_place",
	"address":    "trader_address",
}

// traderStampFromVars extracts the trader block from the case variables and
// maps it onto the fixed trader_* metadata keys. The block is a top-level
// `trader` variable or nested inside one of the request variables the
// finance case services submit (`cancellationRequest.trader`,
// `closingRequest.trader` / `closingMeta.trader` for the closing flow). Only
// keys with a value are set; an absent trader block returns nil so callers
// leave their metadata untouched.
func traderStampFromVars(vars map[string]any) map[string]string {
	for _, container := range []string{"", "cancellationRequest", "closingRequest", "closingMeta"} {
		var raw map[string]any
		var ok bool
		if container == "" {
			raw, ok = vars["trader"].(map[string]any)
		} else if nested, nestedOK := vars[container].(map[string]any); nestedOK {
			raw, ok = nested["trader"].(map[string]any)
		}
		if !ok {
			continue
		}
		stamp := traderStamp(raw)
		if stamp != nil {
			return stamp
		}
	}
	return nil
}

// traderStamp maps one trader object onto the fixed trader_* metadata keys.
func traderStamp(raw map[string]any) map[string]string {
	stamp := map[string]string{}
	for varKey, metaKey := range traderStampKeys {
		if v, ok := raw[varKey].(string); ok && strings.TrimSpace(v) != "" {
			stamp[metaKey] = strings.TrimSpace(v)
		}
	}
	if len(stamp) == 0 {
		return nil
	}
	return stamp
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
