package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/device"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

// trackedSession is what IAM recorded for a freshly established BFF session.
type trackedSession struct {
	SessionID  string
	DeviceID   string
	DeviceName string
	DeviceType string
}

// trackSession registers the new session and its device with IAM. The device
// description comes from the User-Agent (internal/device); the device token is
// the long-lived browser cookie that lets IAM recognise a trusted device for
// MFA bypass.
func (h *BFFHandler) trackSession(r *http.Request, userID, deviceToken string, trustForMFA bool) (*trackedSession, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ua := r.UserAgent()
	ip := extractIP(r)
	info := device.Parse(ua)
	iamReq := &iamclient.CreateSessionRequest{
		UserID:      userID,
		IPAddress:   ip,
		UserAgent:   ua,
		DeviceName:  info.Name,
		DeviceType:  info.Type,
		OS:          info.OS,
		Browser:     info.Browser,
		Fingerprint: device.Fingerprint(ua, ip),
		DeviceToken: deviceToken,
		TrustForMFA: trustForMFA,
	}
	result, err := h.iamClient.CreateSession(ctx, iamReq)
	if err != nil {
		return nil, err
	}
	h.logger.Info("session tracked", "session_id", result.SessionID, "device_id", result.DeviceID)
	return &trackedSession{
		SessionID:  result.SessionID,
		DeviceID:   result.DeviceID,
		DeviceName: info.Name,
		DeviceType: info.Type,
	}, nil
}

func (h *BFFHandler) setDeviceCookie(w http.ResponseWriter, token string) {
	if token == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    token,
		Path:     "/",
		Domain:   h.cfg.SessionCookieDomain,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: parseSameSite(h.cfg.CookieSameSite),
		MaxAge:   deviceCookieMaxAge,
	})
}

func (h *BFFHandler) readDeviceCookie(r *http.Request) string {
	c, err := r.Cookie(deviceCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func generateDeviceToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return session.NewID()
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
