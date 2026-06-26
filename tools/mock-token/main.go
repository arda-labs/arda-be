package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	secret := envOr("JWT_SECRET", "super-secret-dev-key-change-in-production")
	issuer := envOr("JWT_ISSUER", "http://hydra.local")
	audience := envOr("JWT_AUDIENCE", "arda-api")
	subject := envOr("SUBJECT", "dev-admin-sub")
	addr := envOr("HTTP_ADDR", "0.0.0.0:8080")

	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		})

		raw, err := tok.SignedString([]byte(secret))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"%s","token_type":"Bearer","expires_in":%d}`+"\n", raw, int((24*time.Hour).Seconds()))
	})

	log.Printf("mock-token listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
