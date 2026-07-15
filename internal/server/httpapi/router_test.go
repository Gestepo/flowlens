package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"flowlens/internal/server/httpapi"

	"github.com/stretchr/testify/require"
)

func TestRouterMountsAgentBatchEndpoint(t *testing.T) {
	ingest := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	router := httpapi.NewRouter(ingest)

	post := httptest.NewRecorder()
	router.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/agent/batches", nil))
	require.Equal(t, http.StatusAccepted, post.Code)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/agent/batches", nil))
	require.Equal(t, http.StatusMethodNotAllowed, get.Code)
}

func TestAppRouterServesHealthAndSPAFallback(t *testing.T) {
	webDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(webDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<main>FlowLens</main>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("console.log('flowlens')"), 0o644))
	router := httpapi.NewAppRouter(http.NotFoundHandler(), webDir)

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, health.Code)
	require.JSONEq(t, `{"status":"ok"}`, health.Body.String())

	asset := httptest.NewRecorder()
	router.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	require.Equal(t, http.StatusOK, asset.Code)
	require.Equal(t, "console.log('flowlens')", asset.Body.String())

	spa := httptest.NewRecorder()
	router.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "/domains", nil))
	require.Equal(t, http.StatusOK, spa.Code)
	require.Contains(t, spa.Body.String(), "FlowLens")
}

func TestAppRouterMountsOverviewEndpoint(t *testing.T) {
	overview := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router := httpapi.NewAppRouter(http.NotFoundHandler(), t.TempDir(), httpapi.WithOverview(overview))
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/overview?node=flowlens-node-1&range=24h", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestAppRouterMountsGeoIPReloadEndpoint(t *testing.T) {
	reload := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router := httpapi.NewAppRouter(http.NotFoundHandler(), t.TempDir(), httpapi.WithGeoIPReload(reload))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/geoip/reload", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestAppRouterMountsTrafficQueryEndpoints(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router := httpapi.NewAppRouter(http.NotFoundHandler(), t.TempDir(), httpapi.WithTrafficQueries(ok, ok, ok, ok, ok))
	for _, path := range []string{"/api/v1/live", "/api/v1/domains", "/api/v1/owners", "/api/v1/owners/container:web", "/api/v1/flows"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, recorder.Code, path)
	}
}

func TestAppRouterProtectsDashboardAPIsButLeavesAgentAndLoginSeparate(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	protect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Header.Get("X-Test-Session") == "valid" {
				next.ServeHTTP(w, request)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
	router := httpapi.NewAppRouter(ok, t.TempDir(),
		httpapi.WithOverview(ok),
		httpapi.WithBrowserAuth(ok, ok, ok, ok, ok, protect),
	)

	for _, path := range []string{"/api/v1/auth/bootstrap", "/api/v1/auth/login", "/api/v1/agent/batches"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusOK, recorder.Code, path)
	}

	for _, path := range []string{"/api/v1/overview", "/api/v1/session"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, recorder.Code, path)

		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Test-Session", "valid")
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, path)
	}
}

func TestAppRouterProtectsWebhookSettingsEndpoints(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	protect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Header.Get("X-Test-Session") != "valid" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, request)
		})
	}
	router := httpapi.NewAppRouter(ok, t.TempDir(),
		httpapi.WithBrowserAuth(ok, ok, ok, ok, ok, protect),
		httpapi.WithWebhookSettings(ok, ok, ok),
	)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/settings/webhook", nil),
		httptest.NewRequest(http.MethodPut, "/api/v1/settings/webhook", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook/test", nil),
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		request.Header.Set("X-Test-Session", "valid")
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
	}
}

func TestAppRouterProtectsEveryOperationsEndpoint(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	protect := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Header.Get("X-Test-Session") != "valid" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, request)
		})
	}
	router := httpapi.NewAppRouter(ok, t.TempDir(),
		httpapi.WithBrowserAuth(ok, ok, ok, ok, ok, protect),
		httpapi.WithOperations(ok, ok, ok, ok, ok, ok, ok, ok),
	)
	requests := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/health"}, {http.MethodGet, "/api/v1/alerts"}, {http.MethodGet, "/api/v1/alerts/7"},
		{http.MethodGet, "/api/v1/settings"}, {http.MethodGet, "/api/v1/nodes"},
		{http.MethodPut, "/api/v1/settings/retention"}, {http.MethodPut, "/api/v1/settings/alerts"}, {http.MethodPut, "/api/v1/nodes/node-a"},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code, item.path)
		request.Header.Set("X-Test-Session", "valid")
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, item.path)
	}
}
