package middleware

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-auth/jwtverifier"
	"github.com/arda-labs/arda/libs/go/arda-auth/usercontext"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// New creates an HTTP middleware that verifies the JWT Bearer token,
// builds a UserContext from the token claims, and injects it into the request context.
func New(verifier *jwtverifier.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := jwtverifier.ExtractBearer(r.Header.Get("Authorization"))
			if raw == "" {
				ardahttp.WriteProblem(w, r, http.StatusUnauthorized, ardaerrors.New(ardaerrors.CodeUnauthorized, "missing authorization"))
				return
			}

			claims, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				ardahttp.WriteProblem(w, r, http.StatusUnauthorized, ardaerrors.New(ardaerrors.CodeUnauthorized, "invalid token"))
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
