package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := map[string]struct {
		apiKey           string
		upstreamURL      string
		listenAddress    string
		unsetEnv         []string
		expectedError    string
		expectedListen   string
		expectedUpstream string
		expectedAPIKey   string
	}{
		"valid configuration": {
			apiKey:           "test-secret",
			upstreamURL:      "http://127.0.0.1:8000",
			listenAddress:    ":9090",
			expectedListen:   ":9090",
			expectedUpstream: "http://127.0.0.1:8000",
			expectedAPIKey:   "test-secret",
		},
		"default listen address": {
			apiKey:           "test-secret",
			upstreamURL:      "http://127.0.0.1:8000",
			unsetEnv:         []string{"LISTEN_ADDRESS"},
			expectedListen:   defaultListenAddress,
			expectedUpstream: "http://127.0.0.1:8000",
			expectedAPIKey:   "test-secret",
		},
		"https upstream": {
			apiKey:           "test-secret",
			upstreamURL:      "https://inference.example.com",
			unsetEnv:         []string{"LISTEN_ADDRESS"},
			expectedListen:   defaultListenAddress,
			expectedUpstream: "https://inference.example.com",
			expectedAPIKey:   "test-secret",
		},
		"upstream with root path": {
			apiKey:           "test-secret",
			upstreamURL:      "http://127.0.0.1:8000/",
			unsetEnv:         []string{"LISTEN_ADDRESS"},
			expectedListen:   defaultListenAddress,
			expectedUpstream: "http://127.0.0.1:8000/",
			expectedAPIKey:   "test-secret",
		},
		"missing API key": {
			upstreamURL:   "http://127.0.0.1:8000",
			unsetEnv:      []string{"API_KEY"},
			expectedError: "API_KEY",
		},
		"empty API key": {
			upstreamURL:   "http://127.0.0.1:8000",
			expectedError: "API_KEY",
		},
		"whitespace-only API key": {
			apiKey:        "   ",
			upstreamURL:   "http://127.0.0.1:8000",
			expectedError: "API_KEY",
		},
		"leading API key whitespace": {
			apiKey:        " test-secret",
			upstreamURL:   "http://127.0.0.1:8000",
			expectedError: "API_KEY",
		},
		"trailing API key whitespace": {
			apiKey:        "test-secret ",
			upstreamURL:   "http://127.0.0.1:8000",
			expectedError: "API_KEY",
		},
		"missing upstream URL": {
			apiKey:        "test-secret",
			unsetEnv:      []string{"UPSTREAM_URL"},
			expectedError: "UPSTREAM_URL",
		},
		"empty upstream URL": {
			apiKey:        "test-secret",
			expectedError: "UPSTREAM_URL",
		},
		"whitespace-only upstream URL": {
			apiKey:        "test-secret",
			upstreamURL:   "   ",
			expectedError: "UPSTREAM_URL",
		},
		"invalid upstream URL": {
			apiKey:        "test-secret",
			upstreamURL:   "://invalid",
			expectedError: "invalid UPSTREAM_URL",
		},
		"unsupported upstream scheme": {
			apiKey:        "test-secret",
			upstreamURL:   "ftp://127.0.0.1:8000",
			expectedError: "UPSTREAM_URL",
		},
		"missing upstream scheme": {
			apiKey:        "test-secret",
			upstreamURL:   "127.0.0.1:8000",
			expectedError: "UPSTREAM_URL",
		},
		"missing upstream host": {
			apiKey:        "test-secret",
			upstreamURL:   "http://",
			expectedError: "UPSTREAM_URL",
		},
		"upstream credentials": {
			apiKey:        "test-secret",
			upstreamURL:   "http://user:password@127.0.0.1:8000",
			expectedError: "UPSTREAM_URL",
		},
		"upstream query": {
			apiKey:        "test-secret",
			upstreamURL:   "http://127.0.0.1:8000?token=value",
			expectedError: "UPSTREAM_URL",
		},
		"upstream fragment": {
			apiKey:        "test-secret",
			upstreamURL:   "http://127.0.0.1:8000#fragment",
			expectedError: "UPSTREAM_URL",
		},
		"upstream non-root path": {
			apiKey:        "test-secret",
			upstreamURL:   "http://127.0.0.1:8000/v1",
			expectedError: "UPSTREAM_URL",
		},
		"empty listen address": {
			apiKey:        "test-secret",
			upstreamURL:   "http://127.0.0.1:8000",
			expectedError: "LISTEN_ADDRESS",
		},
		"whitespace-only listen address": {
			apiKey:        "test-secret",
			upstreamURL:   "http://127.0.0.1:8000",
			listenAddress: "   ",
			expectedError: "LISTEN_ADDRESS",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Setenv("API_KEY", testCase.apiKey)
			t.Setenv("UPSTREAM_URL", testCase.upstreamURL)
			t.Setenv("LISTEN_ADDRESS", testCase.listenAddress)

			for _, name := range testCase.unsetEnv {
				err := os.Unsetenv(name)
				require.NoError(t, err, "test environment variable must be unset successfully")
			}

			config, err := LoadConfig()

			if testCase.expectedError != "" {
				require.Error(t, err, "invalid configuration must return an error")

				assert.ErrorContains(t, err, testCase.expectedError,
					"configuration error must identify the invalid setting")

				assert.Equal(t, Config{}, config,
					"invalid configuration must not return partially initialized values")

				assert.NotContains(t, err.Error(), "test-secret",
					"configuration errors must not disclose API key contents")
				return
			}

			require.NoError(t, err, "valid configuration must load successfully")

			assert.Equal(t, testCase.expectedListen, config.ListenAddress,
				"configuration must use the expected listen address")

			require.NotNil(t, config.UpstreamURL,
				"valid configuration must contain a parsed upstream URL")

			assert.Equal(t, testCase.expectedUpstream, config.UpstreamURL.String(),
				"configuration must preserve the configured upstream URL")

			assert.Equal(t, testCase.expectedAPIKey, config.APIKey,
				"configuration must preserve the valid API key exactly")
		})
	}
}
