package handler

import "strings"

type resolveKratosIdentityRequest struct {
	IdentityID       string `json:"identity_id"`
	IdentityIDLegacy string `json:"identityId"`
	Email            string `json:"email"`
	Name             string `json:"name"`
}

func (r resolveKratosIdentityRequest) identityID() string {
	return firstNonEmpty(strings.TrimSpace(r.IdentityID), strings.TrimSpace(r.IdentityIDLegacy))
}

type resolveIdentityRequest struct {
	ProviderID       string `json:"provider_id"`
	ProviderIDLegacy string `json:"providerId"`
	ExternalID       string `json:"external_id"`
	ExternalIDLegacy string `json:"externalId"`
	Email            string `json:"email"`
	Name             string `json:"name"`
	EmailVerified    bool   `json:"email_verified"`
	EmailVerifiedLegacy bool `json:"emailVerified"`
}

func (r resolveIdentityRequest) providerID() string {
	return firstNonEmpty(strings.TrimSpace(r.ProviderID), strings.TrimSpace(r.ProviderIDLegacy))
}

func (r resolveIdentityRequest) externalID() string {
	return firstNonEmpty(strings.TrimSpace(r.ExternalID), strings.TrimSpace(r.ExternalIDLegacy))
}

func (r resolveIdentityRequest) emailVerified() bool {
	return r.EmailVerified || r.EmailVerifiedLegacy
}
