package edge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type logChannelWriter chan string

func (writer logChannelWriter) Write(data []byte) (int, error) {
	writer <- string(data)
	return len(data), nil
}

func TestRequestLoggingMiddleware(t *testing.T) {
	const apiKey = "test-secret"

	tests := map[string]struct {
		method         string
		path           string
		authorization  string
		expectedStatus int
	}{
		"authorized request": {
			method:         http.MethodPost,
			path:           "/v1/chat/completions?token=query-secret",
			authorization:  "Bearer " + apiKey,
			expectedStatus: http.StatusOK,
		},
		"unauthorized request": {
			method:         http.MethodGet,
			path:           "/v1/models",
			authorization:  "Bearer wrong-secret",
			expectedStatus: http.StatusUnauthorized,
		},
		"health check": {
			method:         http.MethodGet,
			path:           "/healthz",
			expectedStatus: http.StatusOK,
		},
		"unknown endpoint": {
			method:         http.MethodGet,
			path:           "/private/query-secret",
			expectedStatus: http.StatusNotFound,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			var logOutput bytes.Buffer

			logger := logrus.New()
			logger.SetOutput(&logOutput)
			logger.SetFormatter(&logrus.JSONFormatter{})

			authenticateFn, err := NewAuthenticationMiddleware(apiKey)
			require.NoError(t, err, "valid test API key must initialize authentication middleware")

			var forwardedRequestID string

			inferenceHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwardedRequestID = middleware.GetReqID(r.Context())

				assert.Equal(t, forwardedRequestID, r.Header.Get(requestIDHeader),
					"inference handler must receive the same request ID in context and headers")

				w.WriteHeader(http.StatusOK)
			})

			router := NewInboundRouter(inferenceHandler, authenticateFn)
			handler := NewRequestLoggingMiddleware(logger)(router)

			request := httptest.NewRequest(
				testCase.method,
				testCase.path,
				strings.NewReader("prompt-secret"),
			)

			request.Header.Set(requestIDHeader, "untrusted-client-id")

			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			require.Equal(t, testCase.expectedStatus, recorder.Code,
				"gateway must return the expected HTTP status")

			requestID := recorder.Header().Get(requestIDHeader)
			require.NotEmpty(t, requestID, "gateway must return a generated request ID")

			_, err = uuid.Parse(requestID)
			require.NoError(t, err, "generated request ID must be a valid UUID")

			assert.NotEqual(t, "untrusted-client-id", requestID,
				"gateway must replace a client-provided request ID")

			if forwardedRequestID != "" {
				assert.Equal(t, requestID, forwardedRequestID,
					"response and inference handler must use the same request ID")
			}

			var logEntry map[string]any

			err = json.Unmarshal(logOutput.Bytes(), &logEntry)
			require.NoError(t, err, "access log must contain valid JSON")

			assert.Equal(t, "http_request", logEntry["msg"],
				"access log must identify the HTTP request event")

			assert.Equal(t, "info", logEntry["level"],
				"access log must use the expected logging level")

			assert.Equal(t, requestID, logEntry["request_id"],
				"access log and response must contain the same request ID")

			assert.Equal(t, testCase.method, logEntry["method"],
				"access log must contain the request method")

			assert.Equal(t, float64(testCase.expectedStatus), logEntry["status"],
				"access log must contain the response status")

			assert.Contains(t, logEntry, "duration_ms",
				"access log must contain request duration")

			for _, sensitiveValue := range []string{
				apiKey,
				"wrong-secret",
				"query-secret",
				"prompt-secret",
				"untrusted-client-id",
				"/private/",
			} {
				assert.NotContains(t, logOutput.String(), sensitiveValue,
					"access log must not expose credentials, paths or bodies")
			}
		})
	}
}

func TestRequestLoggingMiddlewareImplicitStatus(t *testing.T) {
	var logOutput bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&logOutput)
	logger.SetFormatter(&logrus.JSONFormatter{})

	emptyHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	handler := NewRequestLoggingMiddleware(logger)(emptyHandler)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Result().StatusCode,
		"HTTP response must default to 200 when the handler does not write")

	var logEntry map[string]any

	err := json.Unmarshal(logOutput.Bytes(), &logEntry)
	require.NoError(t, err, "implicit-status request must produce a valid JSON access log")

	assert.Equal(t, float64(http.StatusOK), logEntry["status"],
		"access log must record HTTP 200 when the handler does not explicitly write a status")

	assert.Equal(t, recorder.Header().Get(requestIDHeader), logEntry["request_id"],
		"access log and response must contain the same generated request ID")
}

func TestRequestLoggingMiddlewareStreaming(t *testing.T) {
	logOutput := make(logChannelWriter, 1)

	logger := logrus.New()
	logger.SetOutput(logOutput)
	logger.SetFormatter(&logrus.JSONFormatter{})

	releaseStream := make(chan struct{})
	var releaseOnce sync.Once

	release := func() {
		releaseOnce.Do(func() {
			close(releaseStream)
		})
	}

	streamingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		_, _ = io.WriteString(w, "data: first-token\n\n")
		w.(http.Flusher).Flush()

		select {
		case <-releaseStream:
		case <-r.Context().Done():
			return
		}

		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})

	handler := NewRequestLoggingMiddleware(logger)(streamingHandler)
	server := httptest.NewServer(handler)

	t.Cleanup(server.Close)
	t.Cleanup(release)

	client := &http.Client{Timeout: 3 * time.Second}

	response, err := client.Get(server.URL + "/v1/chat/completions")
	require.NoError(t, err, "streaming request must reach the HTTP server")
	t.Cleanup(func() { _ = response.Body.Close() })

	require.Equal(t, http.StatusOK, response.StatusCode,
		"streaming response must return HTTP 200")

	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"),
		"streaming response must preserve the SSE content type")

	requestID := response.Header.Get(requestIDHeader)
	require.NotEmpty(t, requestID, "streaming response must contain a request ID")

	reader := bufio.NewReader(response.Body)

	firstEvent, err := reader.ReadString('\n')
	require.NoError(t, err, "first SSE event must arrive before streaming completes")

	assert.Equal(t, "data: first-token\n", firstEvent,
		"logging middleware must not buffer the entire SSE response")

	release()

	remainingBody, err := io.ReadAll(reader)
	require.NoError(t, err, "client must receive the remainder of the SSE stream")

	assert.Contains(t, string(remainingBody), "data: [DONE]\n\n",
		"logging middleware must preserve the final SSE event")

	var loggedLine string

	select {
	case loggedLine = <-logOutput:
	case <-time.After(3 * time.Second):
		t.Fatal("completed streaming request must produce an access log")
	}

	var logEntry map[string]any

	err = json.Unmarshal([]byte(loggedLine), &logEntry)
	require.NoError(t, err, "completed streaming request must produce a valid JSON access log")

	assert.Equal(t, requestID, logEntry["request_id"],
		"streaming access log must contain the response request ID")

	assert.Equal(t, float64(http.StatusOK), logEntry["status"],
		"streaming access log must contain HTTP 200")

	assert.NotContains(t, loggedLine, "first-token",
		"access log must not contain generated model tokens")

	assert.NotContains(t, loggedLine, "[DONE]",
		"access log must not contain SSE response data")
}
