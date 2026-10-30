package edge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthenticationMiddleware(t *testing.T) {
	tests := map[string]struct {
		apiKey    string
		wantError bool
	}{
		"valid API key": {
			apiKey: "test-secret",
		},
		"empty API key": {
			wantError: true,
		},
		"whitespace-only API key": {
			apiKey:    "   ",
			wantError: true,
		},
		"leading whitespace": {
			apiKey:    " test-secret",
			wantError: true,
		},
		"trailing whitespace": {
			apiKey:    "test-secret ",
			wantError: true,
		},
		"surrounding whitespace": {
			apiKey:    "\ttest-secret\n",
			wantError: true,
		},
		"internal whitespace": {
			apiKey: "test secret",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			authenticateFn, err := NewAuthenticationMiddleware(testCase.apiKey)

			if testCase.wantError {
				require.Error(t, err, "invalid API key must prevent middleware initialization")

				assert.Nil(t, authenticateFn,
					"invalid API key must not produce authentication middleware")

				if testCase.apiKey != "" {
					assert.NotContains(t, err.Error(), testCase.apiKey,
						"validation error must not reveal the configured API key")
				}

				return
			}

			require.NoError(t, err, "valid API key must initialize authentication middleware")

			assert.NotNil(t, authenticateFn,
				"valid API key must produce authentication middleware")
		})
	}
}

func TestAuthenticationMiddleware(t *testing.T) {
	const apiKey = "test-secret"

	tests := map[string]struct {
		authorizationHeaders []string
		expectedStatus       int
		expectedForwarded    bool
	}{
		"valid bearer token": {
			authorizationHeaders: []string{"Bearer test-secret"},
			expectedStatus:       http.StatusOK,
			expectedForwarded:    true,
		},
		"lowercase bearer scheme": {
			authorizationHeaders: []string{"bearer test-secret"},
			expectedStatus:       http.StatusOK,
			expectedForwarded:    true,
		},
		"uppercase bearer scheme": {
			authorizationHeaders: []string{"BEARER test-secret"},
			expectedStatus:       http.StatusOK,
			expectedForwarded:    true,
		},
		"missing authorization": {
			expectedStatus: http.StatusUnauthorized,
		},
		"incorrect token": {
			authorizationHeaders: []string{"Bearer wrong-secret"},
			expectedStatus:       http.StatusUnauthorized,
		},
		"empty token": {
			authorizationHeaders: []string{"Bearer "},
			expectedStatus:       http.StatusUnauthorized,
		},
		"basic authentication": {
			authorizationHeaders: []string{"Basic test-secret"},
			expectedStatus:       http.StatusUnauthorized,
		},
		"missing bearer scheme": {
			authorizationHeaders: []string{"test-secret"},
			expectedStatus:       http.StatusUnauthorized,
		},
		"leading header whitespace": {
			authorizationHeaders: []string{" Bearer test-secret"},
			expectedStatus:       http.StatusUnauthorized,
		},
		"trailing token whitespace": {
			authorizationHeaders: []string{"Bearer test-secret "},
			expectedStatus:       http.StatusUnauthorized,
		},
		"multiple token values": {
			authorizationHeaders: []string{"Bearer test-secret other"},
			expectedStatus:       http.StatusUnauthorized,
		},
		"empty authorization header": {
			authorizationHeaders: []string{""},
			expectedStatus:       http.StatusUnauthorized,
		},
		"multiple authorization headers": {
			authorizationHeaders: []string{"Bearer test-secret", "Bearer wrong-secret"},
			expectedStatus:       http.StatusUnauthorized,
		},
		"duplicate valid authorization headers": {
			authorizationHeaders: []string{"Bearer test-secret", "Bearer test-secret"},
			expectedStatus:       http.StatusUnauthorized,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			authenticateFn, err := NewAuthenticationMiddleware(apiKey)
			require.NoError(t, err, "test API key must initialize authentication middleware")

			forwarded := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				forwarded = true
				w.WriteHeader(http.StatusOK)
			})

			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			for _, authorization := range testCase.authorizationHeaders {
				request.Header.Add("Authorization", authorization)
			}

			recorder := httptest.NewRecorder()
			authenticateFn(nextHandler).ServeHTTP(recorder, request)

			require.Equal(t, testCase.expectedStatus, recorder.Code,
				"middleware must return the expected authentication status")

			assert.Equal(t, testCase.expectedForwarded, forwarded,
				"middleware must invoke downstream handler only for authorized requests")

			if !testCase.expectedForwarded {
				assert.NotContains(t, recorder.Body.String(), apiKey,
					"unauthorized response must not reveal the configured API key")
			}
		})
	}
}
