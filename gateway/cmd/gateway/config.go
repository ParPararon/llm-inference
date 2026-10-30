package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const defaultListenAddress = ":8080"

type Config struct {
	ListenAddress string
	UpstreamURL   *url.URL
	APIKey        string
}

func LoadConfig() (Config, error) {
	apiKey, exists := os.LookupEnv("API_KEY")
	if !exists || apiKey == "" || strings.TrimSpace(apiKey) != apiKey {
		return Config{}, errors.New("API_KEY must not be empty or contain surrounding whitespace")
	}

	upstreamAddress, exists := os.LookupEnv("UPSTREAM_URL")
	if !exists || strings.TrimSpace(upstreamAddress) == "" {
		return Config{}, errors.New("UPSTREAM_URL must not be empty")
	}

	upstreamURL, err := url.Parse(upstreamAddress)
	if err != nil {
		return Config{}, fmt.Errorf("invalid UPSTREAM_URL: %w", err)
	}

	if upstreamURL.Scheme != "http" && upstreamURL.Scheme != "https" {
		return Config{}, errors.New("UPSTREAM_URL must use http or https")
	}

	if upstreamURL.Host == "" ||
		upstreamURL.User != nil ||
		upstreamURL.RawQuery != "" ||
		upstreamURL.Fragment != "" {
		return Config{}, errors.New("UPSTREAM_URL must contain only scheme, host and optional root path")
	}

	if upstreamURL.Path != "" && upstreamURL.Path != "/" {
		return Config{}, errors.New("UPSTREAM_URL must not contain a non-root path")
	}

	listenAddress, exists := os.LookupEnv("LISTEN_ADDRESS")
	if !exists {
		listenAddress = defaultListenAddress
	}

	if strings.TrimSpace(listenAddress) == "" {
		return Config{}, errors.New("LISTEN_ADDRESS must not be empty")
	}

	return Config{
		ListenAddress: listenAddress,
		UpstreamURL:   upstreamURL,
		APIKey:        apiKey,
	}, nil
}
