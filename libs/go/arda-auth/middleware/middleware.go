package middleware

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-auth/jwtverifier"
	"github.com/arda-labs/arda/libs/go/arda-auth/usercontext"
)

// New creates an HTTP middleware that verifies the JWT Bearer token,
// builds a UserContext from the token claims, and injects it into the request context.
func New(verifier *jwtverifier.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := jwtverifier.ExtractBearer(r.Header.Get("Authorization"))
			if raw == "" {
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			claims, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			ctx := usercontext.WithContext(r.Context(), &usercontext.UserContext{
				Subject: claims.Subject,
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SubjectFromHeader extracts the X-User-Subject header value.
func SubjectFromHeader(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-User-Subject"))
}
