package edge

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewInboundRouter(
	inferenceHandler http.Handler,
	authenticateFn AuthenticationMiddleware,
) http.Handler {
	router := chi.NewRouter()

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router.With(authenticateFn).Method(http.MethodGet, "/v1/models", inferenceHandler)
	router.With(authenticateFn).Method(http.MethodPost, "/v1/chat/completions", inferenceHandler)
	router.With(authenticateFn).Method(http.MethodPost, "/v1/completions", inferenceHandler)

	return router
}
