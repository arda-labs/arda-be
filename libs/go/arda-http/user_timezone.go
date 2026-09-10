package ardahttp

import (
	"net/http"

	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// HeaderUserTimezone carries the session user's IANA timezone. The BFF
// (auth-gateway) injects it on proxied requests from the session's user
// profile; internal/direct calls omit it and resolve to the platform
// default business timezone.
const HeaderUserTimezone = "X-User-Timezone"

// UserTimezoneMiddleware resolves the user timezone header into a
// *time.Location and stores it in the request context so business-date
// resolution (ardatime.TodayCtx / NowCtx / TZ) follows the requesting user.
// Unknown names fall back to the platform default (never an error here —
// the value was already validated at profile-write time).
func UserTimezoneMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name := r.Header.Get(HeaderUserTimezone); name != "" {
			r = r.WithContext(ardatime.WithTZ(r.Context(), ardatime.InOrDefault(name)))
		}
		next.ServeHTTP(w, r)
	})
}
