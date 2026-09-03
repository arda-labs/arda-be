package catalog

import (
	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
)

// RegisterBuiltinCatalog registers the hand-written SDK methods that do not
// proxy a single internal HTTP route: identity self-service, capability
// listing, knowledge search, and the local export stub. Direct internal
// HTTP reads/mutations come from GeneratedCatalog() — see
// RegisterGeneratedCatalog.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	rag ragSearcher,
) {
	RegisterCRMCatalog(reg)
	RegisterKnowledgeCatalog(reg, rag)
	RegisterIAMCatalog(reg)
}

// compile-time check: the concrete RAG client satisfies the narrow interface
// consumed by the builtin catalog.
var _ ragSearcher = (*svcclient.RAGClient)(nil)
