package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type Payload struct {
	SchemaVersion int               `json:"schema_version"`
	DeliveryID    string            `json:"delivery_id"`
	AlertID       int64             `json:"alert_id"`
	EventType     string            `json:"event_type"`
	Severity      string            `json:"severity"`
	Title         string            `json:"title"`
	Evidence      map[string]string `json:"evidence"`
	OccurredAt    time.Time         `json:"occurred_at"`
	DashboardURL  string            `json:"dashboard_url"`
}

func (payload Payload) IdempotencyKey() string { return payload.DeliveryID }

func EncodeAndSign(payload Payload, secret []byte) ([]byte, string, error) {
	if payload.SchemaVersion != 1 || payload.DeliveryID == "" {
		return nil, "", errors.New("webhook payload identity is required")
	}
	if len(secret) == 0 {
		return nil, "", errors.New("webhook signing secret is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return body, "sha256=" + hex.EncodeToString(mac.Sum(nil)), nil
}
