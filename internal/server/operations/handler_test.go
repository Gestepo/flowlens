package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestHandlerReturnsHealthAlertsSettingsAndNodes(t *testing.T) {
	service := &fakeService{
		health:   HealthResponse{Status: "healthy", Nodes: []Node{{ID: "node-a", Name: "Main", Status: "healthy"}}},
		alerts:   []Alert{{ID: 7, Status: "open", Severity: "warning", Title: "流量异常", Evidence: map[string]string{"node_id": "node-a"}}},
		settings: Settings{DetailRetentionDays: 30, AggregateRetentionMonths: 12},
		nodes:    []Node{{ID: "node-a", Name: "Main", Status: "healthy"}},
	}
	handler := NewHandler(service)
	for _, endpoint := range []struct {
		path     string
		handler  http.HandlerFunc
		contains string
	}{
		{"/api/v1/health", handler.Health, `"status":"healthy"`},
		{"/api/v1/alerts?status=open", handler.Alerts, `"title":"流量异常"`},
		{"/api/v1/settings", handler.Settings, `"detail_retention_days":30`},
		{"/api/v1/nodes", handler.Nodes, `"name":"Main"`},
	} {
		recorder := httptest.NewRecorder()
		endpoint.handler(recorder, httptest.NewRequest(http.MethodGet, endpoint.path, nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), endpoint.contains)
	}
}

func TestHandlerValidatesRetentionAndRenamesNode(t *testing.T) {
	service := &fakeService{}
	handler := NewHandler(service)
	invalid := httptest.NewRecorder()
	handler.Retention(invalid, httptest.NewRequest(http.MethodPut, "/api/v1/settings/retention", bytes.NewBufferString(`{"detail_days":31,"aggregate_months":12}`)))
	require.Equal(t, http.StatusBadRequest, invalid.Code)

	valid := httptest.NewRecorder()
	handler.Retention(valid, httptest.NewRequest(http.MethodPut, "/api/v1/settings/retention", bytes.NewBufferString(`{"detail_days":14,"aggregate_months":6}`)))
	require.Equal(t, http.StatusNoContent, valid.Code)
	require.Equal(t, 14, service.retention.DetailRetentionDays)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/node-a", bytes.NewBufferString(`{"name":"Edge VPS"}`))
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "node-a")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	recorder := httptest.NewRecorder()
	handler.Node(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, "Edge VPS", service.renamed)
}

func TestTrafficFilterUsesOnlyAllowListedEvidence(t *testing.T) {
	filter := TrafficFilter(map[string]string{
		"node_id": "node-a", "owner_id": "container:web", "destination": "1.1.1.1", "secret": "ignore", "url": "https://evil.test",
	})
	require.Equal(t, map[string]string{"node": "node-a", "owner": "container:web", "ip": "1.1.1.1"}, filter)
	encoded, err := json.Marshal(filter)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "evil")
}

type fakeService struct {
	health    HealthResponse
	alerts    []Alert
	settings  Settings
	nodes     []Node
	retention Settings
	renamed   string
}

func (service *fakeService) Health(context.Context, time.Time) (HealthResponse, error) {
	return service.health, nil
}
func (service *fakeService) ListAlerts(context.Context, string, int, int) ([]Alert, error) {
	return service.alerts, nil
}
func (service *fakeService) GetAlert(context.Context, int64) (Alert, error) {
	return service.alerts[0], nil
}
func (service *fakeService) GetSettings(context.Context) (Settings, error) {
	return service.settings, nil
}
func (service *fakeService) UpdateRetention(_ context.Context, detail, months int) error {
	service.retention = Settings{DetailRetentionDays: detail, AggregateRetentionMonths: months}
	return nil
}
func (service *fakeService) UpdateAlertRules(context.Context, []AlertRule) error { return nil }
func (service *fakeService) ListNodes(context.Context, time.Time) ([]Node, error) {
	return service.nodes, nil
}
func (service *fakeService) RenameNode(_ context.Context, _, name string) error {
	service.renamed = name
	return nil
}
