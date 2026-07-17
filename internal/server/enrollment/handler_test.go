package enrollment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnrollmentTokenCanBeRedeemedOnlyOnce(t *testing.T) {
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	handler := New("agent-secret", func() time.Time { return now })

	created := httptest.NewRecorder()
	handler.Create(created, httptest.NewRequest(http.MethodPost, "/api/v1/settings/agent-enrollment", nil))
	require.Equal(t, http.StatusCreated, created.Code)
	var enrollment struct {
		Token     string    `json:"enrollment_token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.NewDecoder(created.Body).Decode(&enrollment))
	require.NotEmpty(t, enrollment.Token)
	require.Equal(t, now.Add(10*time.Minute), enrollment.ExpiresAt)

	redeem := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/enrollment", nil)
	request.Header.Set("Authorization", "Bearer "+enrollment.Token)
	handler.Redeem(redeem, request)
	require.Equal(t, http.StatusOK, redeem.Code)
	var credential struct {
		AgentToken string `json:"agent_token"`
	}
	require.NoError(t, json.NewDecoder(redeem.Body).Decode(&credential))
	require.Equal(t, "agent-secret", credential.AgentToken)

	reused := httptest.NewRecorder()
	handler.Redeem(reused, request)
	require.Equal(t, http.StatusUnauthorized, reused.Code)
}

func TestEnrollmentTokenExpiresWithoutRevealingAgentToken(t *testing.T) {
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	handler := New("agent-secret", func() time.Time { return now })
	created := httptest.NewRecorder()
	handler.Create(created, httptest.NewRequest(http.MethodPost, "/api/v1/settings/agent-enrollment", nil))
	var enrollment struct {
		Token string `json:"enrollment_token"`
	}
	require.NoError(t, json.NewDecoder(created.Body).Decode(&enrollment))
	now = now.Add(11 * time.Minute)

	redeem := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/enrollment", nil)
	request.Header.Set("Authorization", "Bearer "+enrollment.Token)
	handler.Redeem(redeem, request)
	require.Equal(t, http.StatusUnauthorized, redeem.Code)
	require.NotContains(t, redeem.Body.String(), "agent-secret")
}
