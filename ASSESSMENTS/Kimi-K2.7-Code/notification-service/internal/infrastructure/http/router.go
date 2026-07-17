package http

import (
	"net/http"
)

// NewRouter wires the notification endpoints.
func NewRouter(handler *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notifications", handler.Submit)
	mux.HandleFunc("GET /notifications/{id}", handler.GetStatus)
	mux.HandleFunc("GET /notifications", handler.List)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
