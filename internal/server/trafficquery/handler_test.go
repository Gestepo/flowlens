package trafficquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestLiveHandlerParsesFilterAndReturnsJSON(t *testing.T) {
	service := &fakeQueryService{live: Response[LiveItem]{Items: []LiveItem{{ID: "connection-1", DisplayName: "example.com"}}, DataFreshAt: time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)}}
	handler := NewHandler(service, func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) })
	response := httptest.NewRecorder()
	handler.Live(response, httptest.NewRequest(http.MethodGet, "/api/v1/live?node=flowlens-node-1&direction=outbound", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"items":[{"id":"connection-1","observed_at":"0001-01-01T00:00:00Z","direction":"","owner_id":"","owner_name":"","source":"","destination":"","display_name":"example.com","confidence":"","protocol":"","state":"","bytes_sent":0,"bytes_received":0}],"data_fresh_at":"2026-07-14T10:00:00Z","partial_data":null}`, response.Body.String())
	require.Equal(t, "flowlens-node-1", service.filter.NodeID)

	invalid := httptest.NewRecorder()
	handler.Live(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/live?node=flowlens-node-1&limit=999", nil))
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Contains(t, invalid.Body.String(), "invalid_filter")
}

func TestOwnerHandlerDecodesEncodedOwnerID(t *testing.T) {
	service := &fakeQueryService{ownerDetail: OwnerDetail{OwnerItem: OwnerItem{ID: "container:web", Name: "web"}}}
	handler := NewHandler(service, func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/owners/container%3Aweb?node=flowlens-node-1&detail=1", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "container%3Aweb")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	handler.Owner(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "container:web", service.filter.OwnerID)

	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/owners/bad?node=flowlens-node-1&detail=1", nil)
	invalidRoute := chi.NewRouteContext()
	invalidRoute.URLParams.Add("id", "%zz")
	invalid = invalid.WithContext(context.WithValue(invalid.Context(), chi.RouteCtxKey, invalidRoute))
	invalidResponse := httptest.NewRecorder()
	handler.Owner(invalidResponse, invalid)
	require.Equal(t, http.StatusBadRequest, invalidResponse.Code)
}

type fakeQueryService struct {
	live        Response[LiveItem]
	ownerDetail OwnerDetail
	filter      Filter
}

func (service *fakeQueryService) Live(_ context.Context, filter Filter) (Response[LiveItem], error) {
	service.filter = filter
	return service.live, nil
}

func (*fakeQueryService) Domains(context.Context, Filter) (Response[DomainItem], error) {
	return Response[DomainItem]{}, nil
}
func (*fakeQueryService) DomainDetail(context.Context, Filter) (DomainDetail, error) {
	return DomainDetail{}, nil
}
func (*fakeQueryService) Owners(context.Context, Filter) (Response[OwnerItem], error) {
	return Response[OwnerItem]{}, nil
}
func (service *fakeQueryService) OwnerDetail(_ context.Context, filter Filter) (OwnerDetail, error) {
	service.filter = filter
	return service.ownerDetail, nil
}
func (*fakeQueryService) Flows(context.Context, Filter) (Response[FlowItem], error) {
	return Response[FlowItem]{}, nil
}
