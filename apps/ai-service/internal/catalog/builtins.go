package catalog

import (
	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
)

// RegisterBuiltinCatalog registers all default SDK methods grouped by domain:
// crm, knowledge, iam, and finance. Each domain registrar owns its entries and
// dispatchers; this function is the single wiring point.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	crmClient *svcclient.CRMClient,
	financeClient *svcclient.FinanceClient,
	iamClient *svcclient.IAMClient,
	searcher knowledge.Searcher,
) {
	RegisterCRMCatalog(reg, crmClient)
	RegisterKnowledgeCatalog(reg, searcher)
	RegisterIAMCatalog(reg, iamClient)
	RegisterFinanceCatalog(reg, financeClient)
}
