package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type WebhookSettings struct {
	Enabled         bool
	Endpoint        string
	EncryptedSecret []byte
	UpdatedAt       time.Time
}

type SettingsResponse struct {
	Enabled    bool   `json:"enabled"`
	Endpoint   string `json:"endpoint"`
	Configured bool   `json:"configured"`
}

type SettingsStore interface {
	GetSettings(context.Context) (WebhookSettings, error)
	SaveSettings(context.Context, WebhookSettings) error
	QueueTest(context.Context) (int64, error)
}

type SettingsHandler struct {
	store     SettingsStore
	cipher    *SecretCipher
	resolver  Resolver
	allowHTTP bool
}

func NewSettingsHandler(store SettingsStore, key []byte, resolver Resolver, allowHTTP bool) (*SettingsHandler, error) {
	cipher, err := NewSecretCipher(key)
	if err != nil {
		return nil, err
	}
	return &SettingsHandler{store: store, cipher: cipher, resolver: resolver, allowHTTP: allowHTTP}, nil
}

func (handler *SettingsHandler) Get(w http.ResponseWriter, request *http.Request) {
	settings, err := handler.store.GetSettings(request.Context())
	if err != nil {
		writeSettingsError(w, http.StatusInternalServerError, "无法读取 Webhook 设置")
		return
	}
	writeSettingsJSON(w, http.StatusOK, settingsResponse(settings))
}

func (handler *SettingsHandler) Put(w http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(w, request.Body, 64<<10)
	var input struct {
		Enabled  bool   `json:"enabled"`
		Endpoint string `json:"endpoint"`
		Secret   string `json:"secret"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeSettingsError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if input.Enabled || input.Endpoint != "" {
		if _, err := ValidateEndpoint(request.Context(), input.Endpoint, handler.resolver, handler.allowHTTP); err != nil {
			writeSettingsError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	current, err := handler.store.GetSettings(request.Context())
	if err != nil {
		writeSettingsError(w, http.StatusInternalServerError, "无法读取 Webhook 设置")
		return
	}
	encrypted := current.EncryptedSecret
	if input.Secret != "" {
		encrypted, err = handler.cipher.Encrypt([]byte(input.Secret))
		if err != nil {
			writeSettingsError(w, http.StatusInternalServerError, "无法保护 Webhook 密钥")
			return
		}
	}
	if input.Enabled && len(encrypted) == 0 {
		writeSettingsError(w, http.StatusBadRequest, "启用 Webhook 前必须配置密钥")
		return
	}
	settings := WebhookSettings{Enabled: input.Enabled, Endpoint: input.Endpoint, EncryptedSecret: encrypted, UpdatedAt: time.Now().UTC()}
	if err := handler.store.SaveSettings(request.Context(), settings); err != nil {
		writeSettingsError(w, http.StatusInternalServerError, "无法保存 Webhook 设置")
		return
	}
	writeSettingsJSON(w, http.StatusOK, settingsResponse(settings))
}

func (handler *SettingsHandler) Test(w http.ResponseWriter, request *http.Request) {
	settings, err := handler.store.GetSettings(request.Context())
	if err != nil || !settings.Enabled || settings.Endpoint == "" || len(settings.EncryptedSecret) == 0 {
		writeSettingsError(w, http.StatusConflict, "Webhook 尚未启用")
		return
	}
	id, err := handler.store.QueueTest(request.Context())
	if err != nil {
		writeSettingsError(w, http.StatusInternalServerError, "无法创建测试投递")
		return
	}
	writeSettingsJSON(w, http.StatusAccepted, map[string]int64{"delivery_id": id})
}

func settingsResponse(settings WebhookSettings) SettingsResponse {
	return SettingsResponse{Enabled: settings.Enabled, Endpoint: settings.Endpoint, Configured: len(settings.EncryptedSecret) > 0}
}

func writeSettingsError(w http.ResponseWriter, status int, message string) {
	writeSettingsJSON(w, status, map[string]any{"error": map[string]string{"code": "invalid_webhook_settings", "message": message}})
}

func writeSettingsJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
