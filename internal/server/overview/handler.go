package overview

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type SummaryService interface {
	Summary(context.Context, string, string) (Overview, error)
}

type Handler struct {
	service SummaryService
}

func NewHandler(service SummaryService) *Handler {
	return &Handler{service: service}
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		handlerError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method must be GET")
		return
	}
	nodeID := r.URL.Query().Get("node")
	if nodeID == "" {
		handlerError(w, http.StatusBadRequest, "missing_node", "node query parameter is required")
		return
	}
	requestedRange := r.URL.Query().Get("range")
	if requestedRange == "" {
		requestedRange = "24h"
	}
	result, err := handler.service.Summary(r.Context(), nodeID, requestedRange)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRange):
			handlerError(w, http.StatusBadRequest, "invalid_range", "range must be one of 1h, 24h, 7d, or 30d")
		case errors.Is(err, ErrNodeNotFound):
			handlerError(w, http.StatusNotFound, "node_not_found", "node does not exist")
		default:
			handlerError(w, http.StatusInternalServerError, "query_error", "unable to load overview")
		}
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=5")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

func handlerError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
