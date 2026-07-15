package overview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandlerReturnsOverviewJSON(t *testing.T) {
	service := &fakeSummaryService{result: Overview{
		NodeID:        "flowlens-node-1",
		Range:         "24h",
		InboundBytes:  128600000000,
		OutboundBytes: 342100000000,
		Series:        []Point{},
		DataFreshAt:   time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
	}}
	handler := NewHandler(service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/overview?node=flowlens-node-1&range=24h", nil)

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "private, max-age=5", recorder.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"node_id":"flowlens-node-1","range":"24h","inbound_bytes":128600000000,"outbound_bytes":342100000000,"active_connections":0,"domain_coverage":null,"series":[],"data_fresh_at":"2026-07-14T12:00:00Z"}`, recorder.Body.String())
	require.Equal(t, "flowlens-node-1", service.nodeID)
	require.Equal(t, "24h", service.requestedRange)
}

func TestHandlerReturnsStructuredRangeAndNodeErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "range", err: ErrInvalidRange, wantStatus: http.StatusBadRequest, wantCode: "invalid_range"},
		{name: "node", err: ErrNodeNotFound, wantStatus: http.StatusNotFound, wantCode: "node_not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(&fakeSummaryService{err: test.err})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/overview?node=flowlens-node-1&range=90d", nil))

			require.Equal(t, test.wantStatus, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.wantCode)
		})
	}
}

type fakeSummaryService struct {
	result         Overview
	err            error
	nodeID         string
	requestedRange string
}

func (service *fakeSummaryService) Summary(_ context.Context, nodeID, requestedRange string) (Overview, error) {
	service.nodeID = nodeID
	service.requestedRange = requestedRange
	return service.result, service.err
}
