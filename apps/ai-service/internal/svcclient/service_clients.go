package svcclient

import "net/http"

// NewServiceClients builds one signed transport client per configured service
// base URL. The map key is the canonical service name used by the generated
// catalog (x-ai-tool.service / GeneratedEntry.Service) and becomes the HMAC
// audience, so it must match the RequireServiceAuth service name of the target.
//
// A service absent from the map (or present with an empty URL) is not wired in
// this deployment; the catalog registers nothing for it and reports the gap
// instead of failing silently.
func NewServiceClients(urls map[string]string, source, secret string, hc *http.Client) map[string]*Client {
	clients := make(map[string]*Client, len(urls))
	for service, baseURL := range urls {
		if baseURL == "" {
			continue
		}
		clients[service] = NewClient(service, baseURL, source, secret, hc)
	}
	return clients
}
