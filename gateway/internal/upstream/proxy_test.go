package upstream

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type receivedRequest struct {
	method        string
	path          string
	query         string
	authorization string
	body          string
}

type proxyRequestOptions struct {
	method        string
	path          string
	body          string
	authorization string
}

func TestNewReverseProxy(t *testing.T) {
	const clientAPIKey = "client-secret"

	tests := map[string]struct {
		request               proxyRequestOptions
		backendAPIKey         string
		expectedAuthorization string
	}{
		"chat completion without backend authentication": {
			request: proxyRequestOptions{
				method:        http.MethodPost,
				path:          "/v1/chat/completions",
				body:          `{"prompt":"prompt-marker"}`,
				authorization: "Bearer " + clientAPIKey,
			},
		},
		"completion with backend authentication": {
			request: proxyRequestOptions{
				method:        http.MethodPost,
				path:          "/v1/completions",
				body:          `{"prompt":"prompt-marker"}`,
				authorization: "Bearer " + clientAPIKey,
			},
			backendAPIKey:         "backend-secret",
			expectedAuthorization: "Bearer backend-secret",
		},
		"models with query parameters": {
			request: proxyRequestOptions{
				method:        http.MethodGet,
				path:          "/v1/models?limit=10",
				authorization: "Bearer " + clientAPIKey,
			},
			backendAPIKey:         "backend-secret",
			expectedAuthorization: "Bearer backend-secret",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			receivedRequests := make(chan receivedRequest, 1)

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "failed to read request", http.StatusBadRequest)
					return
				}

				receivedRequests <- receivedRequest{
					method:        r.Method,
					path:          r.URL.Path,
					query:         r.URL.RawQuery,
					authorization: r.Header.Get("Authorization"),
					body:          string(body),
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"message":"completion-marker"}`)
			}))
			t.Cleanup(backend.Close)

			proxy := newTestReverseProxy(t, backend.URL, testCase.backendAPIKey)
			recorder := doProxyRequest(t, proxy, testCase.request)

			require.Equal(t, http.StatusCreated, recorder.Code,
				"reverse proxy must preserve the upstream HTTP status")

			assert.JSONEq(t, `{"message":"completion-marker"}`, recorder.Body.String(),
				"reverse proxy must preserve the upstream response body")

			select {
			case received := <-receivedRequests:
				expectedURL, err := url.Parse(testCase.request.path)
				require.NoError(t, err, "test request URL must be valid")

				assert.Equal(t, testCase.request.method, received.method,
					"reverse proxy must preserve the HTTP method")

				assert.Equal(t, expectedURL.Path, received.path,
					"reverse proxy must preserve the OpenAI-compatible request path")

				assert.Equal(t, expectedURL.RawQuery, received.query,
					"reverse proxy must preserve the request query parameters")

				assert.Equal(t, testCase.request.body, received.body,
					"reverse proxy must forward the request body without modification")

				assert.Equal(t, testCase.expectedAuthorization, received.authorization,
					"upstream must receive only the configured backend credential")

				assert.NotEqual(t, "Bearer "+clientAPIKey, received.authorization,
					"client credentials must never be forwarded to the backend")

			case <-time.After(3 * time.Second):
				t.Fatal("backend must receive the proxied request")
			}
		})
	}
}

func TestNewReverseProxyBackendUnavailable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	backendURL := backend.URL
	backend.Close()

	proxy := newTestReverseProxy(t, backendURL, "")

	recorder := doProxyRequest(t, proxy, proxyRequestOptions{
		method: http.MethodGet,
		path:   "/v1/models",
	})

	require.Equal(t, http.StatusBadGateway, recorder.Code,
		"unavailable backend must produce HTTP 502")

	assert.NotContains(t, recorder.Body.String(), backendURL,
		"gateway error response must not expose the backend address")
}

func TestNewReverseProxyStreaming(t *testing.T) {
	releaseBackend := make(chan struct{})
	var releaseOnce sync.Once

	release := func() {
		releaseOnce.Do(func() {
			close(releaseBackend)
		})
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		_, _ = io.WriteString(w, "data: first-token\n\n")
		w.(http.Flusher).Flush()

		select {
		case <-releaseBackend:
		case <-r.Context().Done():
			return
		}

		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))

	t.Cleanup(backend.Close)
	t.Cleanup(release)

	proxy := newTestReverseProxy(t, backend.URL, "")
	gateway := httptest.NewServer(proxy)
	t.Cleanup(gateway.Close)

	client := &http.Client{Timeout: 3 * time.Second}

	response, err := client.Post(
		gateway.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(`{"stream":true}`),
	)
	require.NoError(t, err, "streaming request must reach the gateway")
	t.Cleanup(func() { _ = response.Body.Close() })

	require.Equal(t, http.StatusOK, response.StatusCode,
		"streaming response must preserve HTTP 200")

	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"),
		"streaming response must preserve the SSE content type")

	reader := bufio.NewReader(response.Body)

	firstEvent, err := reader.ReadString('\n')
	require.NoError(t, err, "first SSE event must arrive before backend completion")

	assert.Equal(t, "data: first-token\n", firstEvent,
		"gateway must flush the first SSE event without waiting for the final event")

	release()

	remainingBody, err := io.ReadAll(reader)
	require.NoError(t, err, "remaining SSE events must be readable")

	assert.Contains(t, string(remainingBody), "data: [DONE]\n\n",
		"gateway must forward the final SSE event")
}

func TestNewReverseProxyCancellation(t *testing.T) {
	backendStarted := make(chan struct{})
	backendCancelled := make(chan struct{})

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(backendStarted)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: started\n\n")
		w.(http.Flusher).Flush()

		<-r.Context().Done()
		close(backendCancelled)
	}))
	t.Cleanup(backend.Close)

	proxy := newTestReverseProxy(t, backend.URL, "")
	gateway := httptest.NewServer(proxy)
	t.Cleanup(gateway.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		gateway.URL+"/v1/chat/completions",
		strings.NewReader(`{"stream":true}`),
	)
	require.NoError(t, err, "cancellable test request must be created")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	require.NoError(t, err, "streaming request must reach the gateway")
	t.Cleanup(func() { _ = response.Body.Close() })

	require.Equal(t, http.StatusOK, response.StatusCode,
		"gateway must establish the streaming response")

	select {
	case <-backendStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("backend must start processing the streaming request")
	}

	cancel()

	select {
	case <-backendCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("client cancellation must propagate to the upstream request")
	}
}

func newTestReverseProxy(t *testing.T, backendURL string, backendAPIKey string) http.Handler {
	t.Helper()

	targetURL, err := url.Parse(backendURL)
	require.NoError(t, err, "test backend URL must be valid")

	return NewReverseProxy(targetURL, backendAPIKey)
}

func doProxyRequest(t *testing.T, proxy http.Handler, options proxyRequestOptions) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(options.method, options.path, strings.NewReader(options.body))

	if options.authorization != "" {
		request.Header.Set("Authorization", options.authorization)
	}

	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, request)

	return recorder
}
