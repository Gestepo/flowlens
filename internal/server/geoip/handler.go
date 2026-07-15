package geoip

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type Reloader interface {
	Reload(countryPath, asnPath string) error
}

type ReloadHandler struct {
	tokenHash   [32]byte
	countryPath string
	asnPath     string
	reloader    Reloader
}

func NewReloadHandler(token, countryPath, asnPath string, reloader Reloader) *ReloadHandler {
	return &ReloadHandler{tokenHash: sha256.Sum256([]byte(token)), countryPath: countryPath, asnPath: asnPath, reloader: reloader}
}

func (handler *ReloadHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "method_not_allowed"})
		return
	}
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	provided := sha256.Sum256([]byte(token))
	if token == "" || subtle.ConstantTimeCompare(handler.tokenHash[:], provided[:]) != 1 {
		response.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "unauthorized"})
		return
	}
	if err := handler.reloader.Reload(handler.countryPath, handler.asnPath); err != nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "reload_failed"})
		return
	}
	_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok"})
}
