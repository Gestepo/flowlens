package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Delivery struct {
	ID         int64             `json:"id,omitempty"`
	PublicID   string            `json:"public_id,omitempty"`
	AlertID    int64             `json:"alert_id,omitempty"`
	EventType  string            `json:"event_type"`
	Severity   string            `json:"severity"`
	Title      string            `json:"title"`
	Evidence   map[string]string `json:"evidence"`
	OccurredAt time.Time         `json:"occurred_at"`
	Attempt    int               `json:"attempt,omitempty"`
}

type DeliveryStore interface {
	Claim(context.Context, string, time.Time) (Delivery, bool, error)
	Complete(context.Context, int64, string, int, string, time.Time) error
	Fail(context.Context, int64, string, int, string, string, time.Time, bool) error
}

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Worker struct {
	store        DeliveryStore
	client       Doer
	endpoint     string
	secret       []byte
	dashboardURL string
	now          func() time.Time
}

func NewWorker(store DeliveryStore, client Doer, endpoint string, secret []byte, dashboardURL string, now func() time.Time) *Worker {
	return &Worker{store: store, client: client, endpoint: endpoint, secret: secret, dashboardURL: strings.TrimRight(dashboardURL, "/"), now: now}
}

func (worker *Worker) RunOne(ctx context.Context, owner string) (bool, error) {
	now := worker.now()
	delivery, ok, err := worker.store.Claim(ctx, owner, now)
	if err != nil || !ok {
		return ok, err
	}
	dashboardURL := fmt.Sprintf("%s/alerts/%d", worker.dashboardURL, delivery.AlertID)
	if delivery.EventType == "test" {
		dashboardURL = worker.dashboardURL + "/settings"
	}
	payload := Payload{
		SchemaVersion: 1, DeliveryID: delivery.PublicID, AlertID: delivery.AlertID, EventType: delivery.EventType,
		Severity: delivery.Severity, Title: delivery.Title, Evidence: delivery.Evidence, OccurredAt: delivery.OccurredAt,
		DashboardURL: dashboardURL,
	}
	body, signature, err := EncodeAndSign(payload, worker.secret)
	if err != nil {
		return true, worker.store.Fail(ctx, delivery.ID, owner, 0, err.Error(), "", time.Time{}, true)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, worker.endpoint, bytes.NewReader(body))
	if err != nil {
		return true, worker.store.Fail(ctx, delivery.ID, owner, 0, err.Error(), "", time.Time{}, true)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "FlowLens-Webhook/1")
	request.Header.Set("X-FlowLens-Signature", signature)
	request.Header.Set("Idempotency-Key", payload.IdempotencyKey())

	response, requestErr := worker.client.Do(request)
	status := 0
	excerpt := ""
	if response != nil {
		status = response.StatusCode
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		excerpt = string(data)
		response.Body.Close()
	}
	if requestErr == nil && status >= 200 && status < 300 {
		return true, worker.store.Complete(ctx, delivery.ID, owner, status, excerpt, now)
	}

	attempt := delivery.Attempt + 1
	terminal := attempt >= 6 || (status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests)
	errorMessage := "webhook request failed"
	if requestErr != nil {
		errorMessage = requestErr.Error()
	} else if status != 0 {
		errorMessage = fmt.Sprintf("webhook returned HTTP %d", status)
	}
	next := time.Time{}
	if !terminal {
		delay := retryDelay(attempt)
		if status == http.StatusTooManyRequests {
			if serverDelay := parseRetryAfter(response.Header.Get("Retry-After"), now); serverDelay > delay {
				delay = serverDelay
			}
		}
		next = now.Add(delay)
	}
	return true, worker.store.Fail(ctx, delivery.ID, owner, status, errorMessage, excerpt, next, terminal)
}

func retryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 10 * time.Hour}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(delays) {
		attempt = len(delays)
	}
	return delays[attempt-1]
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
