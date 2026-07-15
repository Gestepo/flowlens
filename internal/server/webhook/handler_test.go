package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettingsHandlerEncryptsAndMasksWebhookSecret(t *testing.T) {
	store := &settingsStore{}
	handler, err := NewSettingsHandler(store, []byte("0123456789abcdef0123456789abcdef"), staticResolver("203.0.113.10"), false)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/webhook", bytes.NewBufferString(`{"enabled":true,"endpoint":"https://hooks.example.test/flowlens","secret":"super-secret-value"}`))
	recorder := httptest.NewRecorder()
	handler.Put(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "super-secret-value")
	require.NotContains(t, string(store.settings.EncryptedSecret), "super-secret-value")
	require.NotEmpty(t, store.settings.EncryptedSecret)

	recorder = httptest.NewRecorder()
	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/webhook", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var response SettingsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Configured)
	require.Equal(t, "https://hooks.example.test/flowlens", response.Endpoint)
}

func TestSettingsHandlerRejectsPrivateEndpointAndQueuesTest(t *testing.T) {
	store := &settingsStore{}
	handler, err := NewSettingsHandler(store, []byte("0123456789abcdef0123456789abcdef"), staticResolver("127.0.0.1"), false)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/webhook", strings.NewReader(`{"enabled":true,"endpoint":"https://localhost/hook","secret":"secret"}`))
	recorder := httptest.NewRecorder()
	handler.Put(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	store.settings = WebhookSettings{Enabled: true, Endpoint: "https://hooks.example.test/flowlens", EncryptedSecret: []byte("encrypted")}
	recorder = httptest.NewRecorder()
	handler.Test(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook/test", nil))
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Equal(t, 1, store.queued)
}

type settingsStore struct {
	settings WebhookSettings
	queued   int
}

func (store *settingsStore) GetSettings(context.Context) (WebhookSettings, error) {
	return store.settings, nil
}
func (store *settingsStore) SaveSettings(_ context.Context, settings WebhookSettings) error {
	store.settings = settings
	return nil
}
func (store *settingsStore) QueueTest(context.Context) (int64, error) {
	store.queued++
	return int64(store.queued), nil
}
