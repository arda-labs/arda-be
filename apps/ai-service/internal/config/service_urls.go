package config

import (
	"os"
	"strings"
)

// ServiceURLEnv maps the canonical service name used across AI contracts
// (contracts/ai-internal/*.json x-ai-tool.service and the generated
// catalog entries) to the environment variable that carries its base URL.
//
// This map is the single source of truth for runtime service support:
//   - the ai-service builds one signed HTTP client per configured URL;
//   - scripts/check-ai-catalog.mjs parses this file and fails when a contract
//     declares a service that is missing here (the tool would otherwise never
//     register);
//   - the same script cross-checks every declared service against the
//     arda-infra ai-service Deployment env.
var ServiceURLEnv = map[string]string{
	"crm-service":          "CRM_SERVICE_URL",
	"finance-service":      "FINANCE_SERVICE_URL",
	"hrm-service":          "HRM_SERVICE_URL",
	"iam-service":          "IAM_SERVICE_URL",
	"workflow-service":     "WORKFLOW_SERVICE_URL",
	"notification-service": "NOTIFICATION_SERVICE_URL",
	"platform-service":     "PLATFORM_SERVICE_URL",
	"mdm-service":          "MDM_SERVICE_URL",
	"loan-service":         "LOAN_SERVICE_URL",
	"deposit-service":      "DEPOSIT_SERVICE_URL",
	"capital-service":      "CAPITAL_SERVICE_URL",
	"statistical-service":  "STATISTICAL_SERVICE_URL",
	"media-service":        "MEDIA_SERVICE_URL",
}

// LoadServiceURLs reads every configured service base URL. Absent variables
// are omitted from the result; callers decide whether an omitted URL is fatal
// (production fails closed for services referenced by the generated catalog).
func LoadServiceURLs() map[string]string {
	urls := make(map[string]string, len(ServiceURLEnv))
	for service, envName := range ServiceURLEnv {
		if value := strings.TrimRight(strings.TrimSpace(os.Getenv(envName)), "/"); value != "" {
			urls[service] = value
		}
	}
	return urls
}
