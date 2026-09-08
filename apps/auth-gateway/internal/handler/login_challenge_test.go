package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
)

func TestValidateLoginChallengeLiveChallenge(t *testing.T) {
	handler := &BFFHandler{
		cfg: config.Config{HydraAdminURL: "http://hydra"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"request_url":"https://hydra/oauth2/auth?client_id=arda-shell"}`)),
				Request:    req,
			}, nil
		})},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login-challenge/validate?login_challenge=challenge-1", nil)
	rec := httptest.NewRecorder()

	handler.ValidateLoginChallenge(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("response did not report a valid challenge: %s", rec.Body.String())
	}
}

func TestValidateLoginChallengeExpiredChallenge(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusConflict, http.StatusGone} {
		handler := &BFFHandler{
			cfg: config.Config{HydraAdminURL: "http://hydra"},
			httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"Not Found"}`)),
					Request:    req,
				}, nil
			})},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/auth/login-challenge/validate?login_challenge=challenge-1", nil)
		rec := httptest.NewRecorder()

		handler.ValidateLoginChallenge(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("hydra status %d: status = %d, want %d", status, rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), `"valid":false`) {
			t.Fatalf("hydra status %d: response did not report invalid challenge: %s", status, rec.Body.String())
		}
	}
}

func TestValidateLoginChallengeHydraUnavailable(t *testing.T) {
	handler := &BFFHandler{
		cfg:    config.Config{HydraAdminURL: "http://hydra"},
		logger: slog.New(slog.DiscardHandler),
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		})},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login-challenge/validate?login_challenge=challenge-1", nil)
	rec := httptest.NewRecorder()

	handler.ValidateLoginChallenge(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestValidateLoginChallengeMissingChallenge(t *testing.T) {
	handler := &BFFHandler{cfg: config.Config{HydraAdminURL: "http://hydra"}}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login-challenge/validate", nil)
	rec := httptest.NewRecorder()

	handler.ValidateLoginChallenge(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAcceptLoginMapsHydraGoneToMachineCode(t *testing.T) {
	handler := &BFFHandler{
		cfg: config.Config{HydraAdminURL: "http://hydra"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"Not Found"}`)),
				Request:    req,
			}, nil
		})},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/kratos/accept-login", strings.NewReader(`{"login_challenge":"challenge-1","subject":"user-1"}`))
	rec := httptest.NewRecorder()

	handler.acceptHydraLogin(rec, req, loginAcceptRequest{LoginChallenge: "challenge-1", Subject: "user-1"})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "login_challenge_expired") {
		t.Fatalf("response did not map to machine error code: %s", rec.Body.String())
	}
}
