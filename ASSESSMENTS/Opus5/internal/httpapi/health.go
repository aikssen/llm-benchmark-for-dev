package httpapi

import (
	"log/slog"
	"maps"
	"net/http"
)

// healthHandler serves the liveness and readiness probes.
//
// The two are separated because they answer different questions and a
// scheduler acts on them differently: a failing liveness probe restarts the
// process, a failing readiness probe only removes it from the load balancer.
type healthHandler struct {
	checker HealthChecker
	logger  *slog.Logger
}

// live handles GET /healthz: is the process running?
func (h *healthHandler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.logger, http.StatusOK, map[string]any{"status": "ok"})
}

// ready handles GET /readyz: should this instance receive traffic?
func (h *healthHandler) ready(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{"status": "ready"}
	maps.Copy(body, h.checker.Details())

	if err := h.checker.Ready(); err != nil {
		body["status"] = "not_ready"
		body["reason"] = err.Error()
		writeJSON(w, h.logger, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, body)
}
