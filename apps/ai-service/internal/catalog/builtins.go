package catalog

// RegisterBuiltinCatalog registers the hand-written SDK methods that do not
// proxy a single internal HTTP route: identity self-service, capability
// listing, knowledge search, and the local export stub. Direct internal
// HTTP reads/mutations come from GeneratedCatalog() — see
// RegisterGeneratedCatalog. The problem-docs lookup registers separately in
// NewCodeModeSuite (RegisterDocsCatalog) so its client stays optional.
func RegisterBuiltinCatalog(
	reg *DispatcherRegistry,
	rag ragSearcher,
) {
	RegisterCRMCatalog(reg)
	RegisterKnowledgeCatalog(reg, rag)
	RegisterIAMCatalog(reg)
}
