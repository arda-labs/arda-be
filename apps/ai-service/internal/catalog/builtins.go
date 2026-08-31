package catalog

import (
	"net/http"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

// RegisterBuiltinCatalog registers all default SDK methods grouped by domain:
// crm, knowledge, iam, and finance. Each domain registrar owns its entries and
// dispatchers; this function is the single wiring point.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	crmBaseURL string,
	financeBaseURL string,
	httpClient *http.Client,
	searcher knowledge.Searcher,
) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	RegisterCRMCatalog(reg, crmBaseURL, httpClient)
	RegisterKnowledgeCatalog(reg, searcher)
	RegisterIAMCatalog(reg)
	RegisterFinanceCatalog(reg, financeBaseURL, httpClient)
}
