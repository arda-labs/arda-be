package svcclient

import (
	"net/http"
)

// HRMClient calls the HRM service internal surface (/internal/ai/*).
// Generated catalog entries ride the generic executor; this typed wrapper
// exists only to give the ClientSet a per-service handle with the right
// signed caller identity.
type HRMClient struct {
	*Client
}

// NewHRMClient returns a typed client for the HRM service.
func NewHRMClient(baseURL, source, secret string, hc *http.Client) *HRMClient {
	return &HRMClient{Client: NewClient("hrm-service", baseURL, source, secret, hc)}
}
