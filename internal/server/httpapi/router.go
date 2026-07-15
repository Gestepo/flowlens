package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

type RouterOption func(*routerConfig)

type routerConfig struct {
	overview    http.Handler
	geoIPReload http.Handler
	live        http.Handler
	domains     http.Handler
	owners      http.Handler
	owner       http.Handler
	flows       http.Handler
	browserAuth *browserAuthConfig
	webhookGet  http.Handler
	webhookPut  http.Handler
	webhookTest http.Handler
	operations  *operationsConfig
}

type browserAuthConfig struct {
	bootstrap http.Handler
	login     http.Handler
	logout    http.Handler
	password  http.Handler
	session   http.Handler
	protect   func(http.Handler) http.Handler
}

type operationsConfig struct {
	health, alerts, alert, settings, nodes, retention, alertSettings, node http.Handler
}

func WithTrafficQueries(live, domains, owners, owner, flows http.Handler) RouterOption {
	return func(config *routerConfig) {
		config.live, config.domains, config.owners, config.owner, config.flows = live, domains, owners, owner, flows
	}
}

func WithGeoIPReload(handler http.Handler) RouterOption {
	return func(config *routerConfig) { config.geoIPReload = handler }
}

func WithOverview(handler http.Handler) RouterOption {
	return func(config *routerConfig) { config.overview = handler }
}

func WithBrowserAuth(bootstrap, login, logout, password, session http.Handler, protect func(http.Handler) http.Handler) RouterOption {
	return func(config *routerConfig) {
		config.browserAuth = &browserAuthConfig{bootstrap: bootstrap, login: login, logout: logout, password: password, session: session, protect: protect}
	}
}

func WithWebhookSettings(get, put, test http.Handler) RouterOption {
	return func(config *routerConfig) { config.webhookGet, config.webhookPut, config.webhookTest = get, put, test }
}

func WithOperations(health, alerts, alert, settings, nodes, retention, alertSettings, node http.Handler) RouterOption {
	return func(config *routerConfig) {
		config.operations = &operationsConfig{health: health, alerts: alerts, alert: alert, settings: settings, nodes: nodes, retention: retention, alertSettings: alertSettings, node: node}
	}
}

func NewRouter(ingest http.Handler) http.Handler {
	router := chi.NewRouter()
	router.Method(http.MethodPost, "/api/v1/agent/batches", ingest)
	return router
}

func NewAppRouter(ingest http.Handler, webDir string, options ...RouterOption) http.Handler {
	config := routerConfig{}
	for _, option := range options {
		option(&config)
	}
	router := chi.NewRouter()
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	router.Method(http.MethodPost, "/api/v1/agent/batches", ingest)
	if config.browserAuth != nil {
		authentication := config.browserAuth
		router.Method(http.MethodPost, "/api/v1/auth/bootstrap", authentication.bootstrap)
		router.Method(http.MethodPost, "/api/v1/auth/login", authentication.login)
		router.With(authentication.protect).Method(http.MethodPost, "/api/v1/auth/logout", authentication.logout)
		router.With(authentication.protect).Method(http.MethodPost, "/api/v1/auth/password", authentication.password)
		router.With(authentication.protect).Method(http.MethodGet, "/api/v1/session", authentication.session)
	}
	if config.geoIPReload != nil {
		router.Method(http.MethodPost, "/api/v1/admin/geoip/reload", config.geoIPReload)
	}
	dashboard := func(routes chi.Router) {
		if config.overview != nil {
			routes.Method(http.MethodGet, "/api/v1/overview", config.overview)
		}
		if config.live != nil {
			routes.Method(http.MethodGet, "/api/v1/live", config.live)
			routes.Method(http.MethodGet, "/api/v1/domains", config.domains)
			routes.Method(http.MethodGet, "/api/v1/owners", config.owners)
			routes.Method(http.MethodGet, "/api/v1/owners/{id}", config.owner)
			routes.Method(http.MethodGet, "/api/v1/flows", config.flows)
		}
		if config.webhookGet != nil {
			routes.Method(http.MethodGet, "/api/v1/settings/webhook", config.webhookGet)
			routes.Method(http.MethodPut, "/api/v1/settings/webhook", config.webhookPut)
			routes.Method(http.MethodPost, "/api/v1/settings/webhook/test", config.webhookTest)
		}
		if config.operations != nil {
			operations := config.operations
			routes.Method(http.MethodGet, "/api/v1/health", operations.health)
			routes.Method(http.MethodGet, "/api/v1/alerts", operations.alerts)
			routes.Method(http.MethodGet, "/api/v1/alerts/{id}", operations.alert)
			routes.Method(http.MethodGet, "/api/v1/settings", operations.settings)
			routes.Method(http.MethodGet, "/api/v1/nodes", operations.nodes)
			routes.Method(http.MethodPut, "/api/v1/settings/retention", operations.retention)
			routes.Method(http.MethodPut, "/api/v1/settings/alerts", operations.alertSettings)
			routes.Method(http.MethodPut, "/api/v1/nodes/{id}", operations.node)
		}
	}
	if config.browserAuth != nil {
		router.Group(func(routes chi.Router) {
			routes.Use(config.browserAuth.protect)
			dashboard(routes)
		})
	} else {
		dashboard(router)
	}
	router.Handle("/*", spaHandler(webDir))
	return router
}

func spaHandler(webDir string) http.Handler {
	root := filepath.Clean(webDir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		relative := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		candidate := filepath.Join(root, relative)
		if withinRoot(root, candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				http.ServeFile(w, r, candidate)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
