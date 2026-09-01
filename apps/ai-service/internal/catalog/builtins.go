package catalog

import (
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

// RegisterBuiltinCatalog registers the hand-written SDK methods that do not
// proxy a single internal HTTP route: identity self-service, capability
// listing, knowledge search, and the local export stub. Direct internal
// HTTP reads/mutations come from GeneratedCatalog() — see
// RegisterGeneratedCatalog.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	searcher knowledge.Searcher,
) {
	RegisterCRMCatalog(reg)
	RegisterKnowledgeCatalog(reg, searcher)
	RegisterIAMCatalog(reg)
}
