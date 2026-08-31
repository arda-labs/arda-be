package identity

import (
	"fmt"
	"net/http"
	"time"
)

// SignRequest attaches a short-lived caller assertion (x-service-auth) to an
// outgoing HTTP request bound for `audience`. The receiving service verifies
// it with RequireServiceAuth / Verify; the signed caller must never be
// overridable by the caller of this function — it is derived solely from
// source + audience.
//
// Replay protection is a bounded window: the token expires after ttl (≤ 5
// minutes) and carries a nonce for audit/correlation, but there is no
// server-side one-time-token store, so the window is reduced, not eliminated.
func SignRequest(req *http.Request, secret, source, audience string, now time.Time, ttl time.Duration) error {
	token, err := Issue(secret, source, audience, now, ttl)
	if err != nil {
		return fmt.Errorf("sign service identity: %w", err)
	}
	req.Header.Set(MetadataKey, token)
	return nil
}
