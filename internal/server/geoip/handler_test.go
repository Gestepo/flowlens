package geoip

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReloadHandlerRequiresBearerTokenAndReloadsConfiguredFiles(t *testing.T) {
	reloader := &fakeReloader{}
	handler := NewReloadHandler("secret-token", "/geo/country.mmdb", "/geo/asn.mmdb", reloader)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/v1/admin/geoip/reload", nil))
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/geoip/reload", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "/geo/country.mmdb", reloader.country)
	require.Equal(t, "/geo/asn.mmdb", reloader.asn)
}

type fakeReloader struct{ country, asn string }

func (reloader *fakeReloader) Reload(country, asn string) error {
	reloader.country, reloader.asn = country, asn
	return nil
}
