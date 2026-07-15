package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEncodeAndSignPayload(t *testing.T) {
	payload := Payload{
		SchemaVersion: 1, DeliveryID: "delivery-42", AlertID: 7, EventType: "opened", Severity: "warning",
		Title: "节点流量异常", Evidence: map[string]string{"node_id": "node-a"},
		OccurredAt: time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC), DashboardURL: "https://flowlens.example/alerts/7",
	}
	body, signature, err := EncodeAndSign(payload, []byte("webhook-signing-secret"))
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	for _, field := range []string{"schema_version", "delivery_id", "alert_id", "event_type", "severity", "title", "evidence", "occurred_at", "dashboard_url"} {
		require.Contains(t, decoded, field)
	}

	mac := hmac.New(sha256.New, []byte("webhook-signing-secret"))
	mac.Write(body)
	require.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), signature)
	require.Equal(t, "delivery-42", payload.IdempotencyKey())
}

func TestEncodeRejectsMissingDeliveryIdentityAndSecret(t *testing.T) {
	_, _, err := EncodeAndSign(Payload{SchemaVersion: 1}, []byte("secret"))
	require.Error(t, err)
	_, _, err = EncodeAndSign(Payload{SchemaVersion: 1, DeliveryID: "delivery"}, nil)
	require.Error(t, err)
}
