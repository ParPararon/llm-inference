package edge

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type AuthenticationMiddleware func(http.Handler) http.Handler

func NewAuthenticationMiddleware(apiKey string) (AuthenticationMiddleware, error) {
	if apiKey == "" || strings.TrimSpace(apiKey) != apiKey {
		return nil, errors.New("authentication API key must not be empty or contain surrounding whitespace")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorizationHeaders := r.Header.Values("Authorization")
			if len(authorizationHeaders) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			token, ok := extractBearerToken(authorizationHeaders[0])
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func extractBearerToken(authorization string) (string, bool) {
	scheme, token, ok := strings.Cut(authorization, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}

	if token == "" || strings.TrimSpace(token) != token {
		return "", false
	}

	return token, true
}
