package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type kratosWhoamiResponse struct {
	Identity struct {
		ID     string `json:"id"`
		Traits struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"traits"`
	} `json:"identity"`
}

func (h *BFFHandler) KratosWhoami(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "sessions/whoami")
}
func (h *BFFHandler) KratosCreateLoginAPIFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratosAPI(w, r, "self-service/login/api")
}
func (h *BFFHandler) KratosCreateLoginFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login/browser")
}
func (h *BFFHandler) KratosGetLoginFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/login/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitLogin(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratosAPI(w, r, "self-service/login?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosCreateSettingsFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/settings/browser")
}
func (h *BFFHandler) KratosGetSettingsFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/settings/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitSettings(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/settings?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosCreateRecoveryFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/recovery/browser")
}
func (h *BFFHandler) KratosGetRecoveryFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/recovery/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitRecovery(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/recovery?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosCreateVerificationFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/verification/browser")
}
func (h *BFFHandler) KratosGetVerificationFlow(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/verification/flows?"+r.URL.RawQuery)
}
func (h *BFFHandler) KratosSubmitVerification(w http.ResponseWriter, r *http.Request) {
	h.proxyToKratos(w, r, "self-service/verification?"+r.URL.RawQuery)
}

func (h *BFFHandler) proxyToKratos(w http.ResponseWriter, r *http.Request, path string) {
	h.proxyToKratosWithOptions(w, r, path, false)
}

func (h *BFFHandler) proxyToKratosAPI(w http.ResponseWriter, r *http.Request, path string) {
	h.proxyToKratosWithOptions(w, r, path, true)
}

func (h *BFFHandler) proxyToKratosWithOptions(w http.ResponseWriter, r *http.Request, path string, stripBrowserHeaders bool) {
	target := fmt.Sprintf("%s/%s", strings.TrimSuffix(h.cfg.KratosPublicURL, "/"), strings.TrimPrefix(path, "/"))
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		respondError(w, http.StatusBadGateway, "kratos proxy request invalid")
		return
	}
	proxyReq.Header = r.Header.Clone()
	if stripBrowserHeaders {
		// Kratos API flows reject browser AJAX requests with Origin/Sec-Fetch headers.
		// The BFF is intentionally converting this same-origin browser call into a
		// server-to-server API-flow request so Kratos can return a session_token.
		proxyReq.Header.Del("Origin")
		proxyReq.Header.Del("Referer")
		proxyReq.Header.Del("Sec-Fetch-Dest")
		proxyReq.Header.Del("Sec-Fetch-Mode")
		proxyReq.Header.Del("Sec-Fetch-Site")
		proxyReq.Header.Del("Cookie")
	}
	proxyReq.Header.Set("X-Forwarded-Proto", "https")
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		respondError(w, http.StatusBadGateway, "kratos proxy failed")
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (h *BFFHandler) kratosWhoami(r *http.Request, sessionToken string) (*kratosWhoamiResponse, error) {
	target := fmt.Sprintf("%s/%s", strings.TrimSuffix(h.cfg.KratosPublicURL, "/"), "sessions/whoami")
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("kratos whoami request invalid")
	}
	proxyReq.Header = r.Header.Clone()
	proxyReq.Header.Set("Accept", "application/json")
	proxyReq.Header.Del("Accept-Encoding")
	if strings.TrimSpace(sessionToken) != "" {
		proxyReq.Header.Set("X-Session-Token", strings.TrimSpace(sessionToken))
	}
	proxyReq.Header.Set("X-Forwarded-Proto", "https")
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("kratos whoami failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kratos session is not authenticated: HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}
	var whoami kratosWhoamiResponse
	if err := json.Unmarshal(body, &whoami); err != nil {
		return nil, fmt.Errorf("decode kratos whoami failed: %s", truncateBody(body))
	}
	if strings.TrimSpace(whoami.Identity.ID) == "" {
		return nil, fmt.Errorf("kratos whoami missing identity: %s", truncateBody(body))
	}
	return &whoami, nil
}

func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		return text[:500]
	}
	if text == "" {
		return "<empty response>"
	}
	return text
}
