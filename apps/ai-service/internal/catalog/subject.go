package catalog

import (
	"sort"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// scopeToMetadata projects the gateway-verified AI scope onto the delegated
// subject forwarded to target services. Only fields derived from the verified
// context are copied — tool arguments can never override the subject.
func scopeToMetadata(scope tools.Context) metadata.Context {
	permissions := make([]string, 0, len(scope.Permissions))
	for p := range scope.Permissions {
		permissions = append(permissions, p)
	}
	sort.Strings(permissions)
	return metadata.Context{
		RequestID:   scope.RequestID,
		TenantID:    scope.TenantID,
		UserID:      scope.ActorUserID,
		ActorUserID: scope.ActorUserID,
		OrgID:       scope.ActiveOrgID,
		OrgIDs:      scope.OrgIDs,
		Roles:       scope.Roles,
		Permissions: permissions,
		AuthChecked: "true",
	}
}
