package catalog

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateTypeDefinitions renders the arda.* SDK surface as a compact
// TypeScript declaration file built from the live catalog — the single source
// of truth. The model gets the whole SDK surface once in context instead of
// re-reading JSDoc for every search() call.
//
// Output shape:
//
//	declare namespace arda {
//	  /** <first line of JSDoc> */
//	  function getCustomer(args: { customerId: string }): Promise<CustomerSummary>;
//	}
func GenerateTypeDefinitions(entries []CatalogEntry) string {
	if len(entries) == 0 {
		return "// No arda.* SDK methods registered."
	}

	// Group by domain for stable, scannable output.
	byDomain := make(map[string][]CatalogEntry)
	for _, entry := range entries {
		domain := entry.Domain
		if domain == "" {
			domain = "sdk"
		}
		byDomain[domain] = append(byDomain[domain], entry)
	}
	domains := make([]string, 0, len(byDomain))
	for domain := range byDomain {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	var b strings.Builder
	b.WriteString("declare namespace arda {\n")
	for _, domain := range domains {
		group := byDomain[domain]
		sort.Slice(group, func(i, j int) bool { return group[i].MethodName < group[j].MethodName })
		fmt.Fprintf(&b, "  namespace %s {\n", domain)
		emitted := make(map[string]bool)
		for _, entry := range group {
			if emitted[entry.MethodName] {
				continue
			}
			emitted[entry.MethodName] = true
			if summary := firstJSDocLine(entry.JSDoc); summary != "" {
				fmt.Fprintf(&b, "    /** %s */\n", summary)
			}
			sig := entry.Signature
			if sig == "" {
				sig = entry.SDKPath
			}
			// Signatures are stored as "arda.<domain>.<method>(...)" — strip the
			// namespace prefix so the declaration sits inside its domain block.
			sig = strings.TrimPrefix(sig, "arda."+domain+".")
			if !strings.HasSuffix(sig, ";") {
				sig += ";"
			}
			b.WriteString("    ")
			b.WriteString(sig)
			b.WriteString("\n")
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}")
	return b.String()
}
