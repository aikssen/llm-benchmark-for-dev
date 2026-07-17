package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"notification-service/internal/repository"
	"notification-service/internal/service"
)

// Handler handles HTTP requests for the notification service
type Handler struct {
	orchestrator *service.Orchestrator
	repo         repository.Repository
}

// NewHandler creates a new Handler instance
func NewHandler(orchestrator *service.Orchestrator, repo repository.Repository) *Handler {
	return &Handler{
		orchestrator: orchestrator,
		repo:         repo,
	}
}

// SubmitNotification handles POST /notifications
func (h *Handler) SubmitNotification(w http.ResponseWriter, r *http.Request) {
	var req service.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := h.orchestrator.Submit(r.Context(), &req)
	if err != nil {
		// Differentiate client validation errors from system failures
		if isClientError(err) {
			respondWithError(w, http.StatusBadRequest, err.Error())
		} else {
			respondWithError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	respondWithJSON(w, http.StatusAccepted, res)
}

// GetNotification handles GET /notifications/{id}
func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "missing notification ID")
		return
	}

	n, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "notification not found")
		} else {
			respondWithError(w, http.StatusInternalServerError, "failed to retrieve notification")
		}
		return
	}

	respondWithJSON(w, http.StatusOK, n)
}

// ListNotifications handles GET /notifications
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.List(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	respondWithJSON(w, http.StatusOK, list)
}

// Helpers

func isClientError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "is required") ||
		strings.Contains(msg, "cannot be empty") ||
		strings.Contains(msg, "unsupported channel")
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}
