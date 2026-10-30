package edge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInboundRouter(t *testing.T) {
	const apiKey = "test-secret"

	tests := map[string]struct {
		method            string
		path              string
		authorization     string
		expectedStatus    int
		expectedForwarded bool
	}{
		"health check without authentication": {
			method:         http.MethodGet,
			path:           "/healthz",
			expectedStatus: http.StatusOK,
		},
		"models endpoint": {
			method:            http.MethodGet,
			path:              "/v1/models",
			authorization:     "Bearer " + apiKey,
			expectedStatus:    http.StatusOK,
			expectedForwarded: true,
		},
		"chat completions endpoint": {
			method:            http.MethodPost,
			path:              "/v1/chat/completions",
			authorization:     "Bearer " + apiKey,
			expectedStatus:    http.StatusOK,
			expectedForwarded: true,
		},
		"completions endpoint": {
			method:            http.MethodPost,
			path:              "/v1/completions",
			authorization:     "Bearer " + apiKey,
			expectedStatus:    http.StatusOK,
			expectedForwarded: true,
		},
		"missing authentication": {
			method:         http.MethodPost,
			path:           "/v1/chat/completions",
			expectedStatus: http.StatusUnauthorized,
		},
		"invalid authentication": {
			method:         http.MethodGet,
			path:           "/v1/models",
			authorization:  "Bearer wrong-secret",
			expectedStatus: http.StatusUnauthorized,
		},
		"unsupported endpoint": {
			method:         http.MethodPost,
			path:           "/v1/embeddings",
			authorization:  "Bearer " + apiKey,
			expectedStatus: http.StatusNotFound,
		},
		"unsupported method": {
			method:         http.MethodDelete,
			path:           "/v1/models",
			authorization:  "Bearer " + apiKey,
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			authenticateFn, err := NewAuthenticationMiddleware(apiKey)
			require.NoError(t, err, "valid test API key must initialize middleware")

			forwarded := false
			inferenceHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				forwarded = true
				w.WriteHeader(http.StatusOK)
			})

			router := NewInboundRouter(inferenceHandler, authenticateFn)

			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, testCase.expectedStatus, recorder.Code,
				"router must return the expected HTTP status")

			assert.Equal(t, testCase.expectedForwarded, forwarded,
				"router must forward only authorized requests to supported inference endpoints")
		})
	}
}
