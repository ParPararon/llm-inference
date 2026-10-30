package upstream

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

func NewReverseProxy(targetURL *url.URL, backendAPIKey string) http.Handler {
	return configureReverseProxy(targetURL, backendAPIKey)
}

func configureReverseProxy(targetURL *url.URL, backendAPIKey string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			configureOutboundRequest(request, targetURL, backendAPIKey)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
}

func configureOutboundRequest(request *httputil.ProxyRequest, targetURL *url.URL, backendAPIKey string) {
	request.SetURL(targetURL)
	request.Out.Header.Del("Authorization")

	if backendAPIKey == "" {
		return
	}

	request.Out.Header.Set("Authorization", "Bearer "+backendAPIKey)
}
