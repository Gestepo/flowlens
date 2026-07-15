package trafficquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

type QueryService interface {
	Live(context.Context, Filter) (Response[LiveItem], error)
	Domains(context.Context, Filter) (Response[DomainItem], error)
	DomainDetail(context.Context, Filter) (DomainDetail, error)
	Owners(context.Context, Filter) (Response[OwnerItem], error)
	OwnerDetail(context.Context, Filter) (OwnerDetail, error)
	Flows(context.Context, Filter) (Response[FlowItem], error)
}

type Handler struct {
	service QueryService
	now     func() time.Time
}

func NewHandler(service QueryService, now func() time.Time) *Handler {
	return &Handler{service: service, now: now}
}

func (handler *Handler) Live(response http.ResponseWriter, request *http.Request) {
	serveQuery(handler, response, request, true, handler.service.Live)
}

func (handler *Handler) Domains(response http.ResponseWriter, request *http.Request) {
	if request.URL.Query().Get("detail") == "1" {
		filter, err := ParseFilter(request.URL.Query(), handler.now().UTC(), true)
		if err != nil || filter.Domain == "" || filter.Direction == "" || filter.Confidence == "" {
			writeQueryError(response, http.StatusBadRequest, "invalid_filter", "domain detail requires domain, direction, and confidence")
			return
		}
		result, err := handler.service.DomainDetail(request.Context(), filter)
		if err != nil {
			writeQueryError(response, http.StatusInternalServerError, "query_failed", "unable to load domain detail")
			return
		}
		writeQueryJSON(response, result)
		return
	}
	serveQuery(handler, response, request, false, handler.service.Domains)
}

func (handler *Handler) Owners(response http.ResponseWriter, request *http.Request) {
	serveQuery(handler, response, request, false, handler.service.Owners)
}

func (handler *Handler) Owner(response http.ResponseWriter, request *http.Request) {
	filter, err := ParseFilter(request.URL.Query(), handler.now().UTC(), true)
	if err != nil {
		writeQueryError(response, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	ownerID, err := url.PathUnescape(chi.URLParam(request, "id"))
	if err != nil || ownerID == "" || len(ownerID) > 256 {
		writeQueryError(response, http.StatusBadRequest, "invalid_filter", "owner identifier is invalid")
		return
	}
	filter.OwnerID = ownerID
	if request.URL.Query().Get("detail") == "1" {
		result, err := handler.service.OwnerDetail(request.Context(), filter)
		if err != nil {
			writeQueryError(response, http.StatusInternalServerError, "query_failed", "unable to load owner detail")
			return
		}
		writeQueryJSON(response, result)
		return
	}
	result, err := handler.service.Owners(request.Context(), filter)
	if err != nil {
		writeQueryError(response, http.StatusInternalServerError, "query_failed", "unable to load owner")
		return
	}
	writeQueryJSON(response, result)
}

func (handler *Handler) Flows(response http.ResponseWriter, request *http.Request) {
	serveQuery(handler, response, request, true, handler.service.Flows)
}

func serveQuery[T any](handler *Handler, response http.ResponseWriter, request *http.Request, detail bool, query func(context.Context, Filter) (Response[T], error)) {
	filter, err := ParseFilter(request.URL.Query(), handler.now().UTC(), detail)
	if err != nil {
		writeQueryError(response, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	result, err := query(request.Context(), filter)
	if err != nil {
		writeQueryError(response, http.StatusInternalServerError, "query_failed", "unable to load traffic data")
		return
	}
	writeQueryJSON(response, result)
}

func writeQueryJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(value)
}

func writeQueryError(response http.ResponseWriter, status int, code, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": code, "message": message})
}
