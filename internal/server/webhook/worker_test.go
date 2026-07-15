package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkerSignsAndCompletesSuccessfulDelivery(t *testing.T) {
	store := &workerStore{delivery: deliveryFixture()}
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, "delivery-7", request.Header.Get("Idempotency-Key"))
		require.NotEmpty(t, request.Header.Get("X-FlowLens-Signature"))
		require.Contains(t, string(body), `"alert_id":42`)
		return response(http.StatusNoContent, ""), nil
	})
	worker := NewWorker(store, client, "https://hooks.example.test/flowlens", []byte("signing-secret"), "https://flowlens.example", fixedWorkerNow)

	worked, err := worker.RunOne(context.Background(), "worker-a")
	require.NoError(t, err)
	require.True(t, worked)
	require.True(t, store.completed)
}

func TestWorkerRetriesTransientFailuresAndTerminatesPermanentFailures(t *testing.T) {
	tests := []struct {
		name      string
		client    Doer
		attempt   int
		terminal  bool
		wantDelay time.Duration
	}{
		{"server error", doerFunc(func(*http.Request) (*http.Response, error) { return response(503, "unavailable"), nil }), 0, false, time.Minute},
		{"rate limit", doerFunc(func(*http.Request) (*http.Response, error) {
			value := response(429, "slow down")
			value.Header.Set("Retry-After", "600")
			return value, nil
		}), 0, false, 10 * time.Minute},
		{"network error", doerFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("timeout") }), 1, false, 5 * time.Minute},
		{"client error", doerFunc(func(*http.Request) (*http.Response, error) { return response(400, "bad request"), nil }), 0, true, 0},
		{"sixth failure", doerFunc(func(*http.Request) (*http.Response, error) { return response(503, "unavailable"), nil }), 5, true, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delivery := deliveryFixture()
			delivery.Attempt = test.attempt
			store := &workerStore{delivery: delivery}
			worker := NewWorker(store, test.client, "https://hooks.example.test/flowlens", []byte("signing-secret"), "https://flowlens.example", fixedWorkerNow)
			worked, err := worker.RunOne(context.Background(), "worker-a")
			require.NoError(t, err)
			require.True(t, worked)
			require.Equal(t, test.terminal, store.terminal)
			if !test.terminal {
				require.Equal(t, fixedWorkerNow().Add(test.wantDelay), store.nextAttempt)
			}
		})
	}
}

func TestWorkerBoundsResponseExcerpt(t *testing.T) {
	store := &workerStore{delivery: deliveryFixture()}
	client := doerFunc(func(*http.Request) (*http.Response, error) { return response(503, strings.Repeat("x", 80<<10)), nil })
	worker := NewWorker(store, client, "https://hooks.example.test/flowlens", []byte("signing-secret"), "https://flowlens.example", fixedWorkerNow)
	_, err := worker.RunOne(context.Background(), "worker-a")
	require.NoError(t, err)
	require.LessOrEqual(t, len(store.excerpt), 64<<10)
}

func deliveryFixture() Delivery {
	return Delivery{ID: 7, PublicID: "delivery-7", AlertID: 42, EventType: "opened", Severity: "warning", Title: "节点流量异常", Evidence: map[string]string{"node_id": "node-a"}, OccurredAt: fixedWorkerNow(), Attempt: 0}
}

func fixedWorkerNow() time.Time { return time.Date(2026, 7, 15, 7, 0, 0, 0, time.UTC) }

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type workerStore struct {
	delivery    Delivery
	claimed     bool
	completed   bool
	terminal    bool
	nextAttempt time.Time
	excerpt     string
}

func (store *workerStore) Claim(context.Context, string, time.Time) (Delivery, bool, error) {
	if store.claimed {
		return Delivery{}, false, nil
	}
	store.claimed = true
	return store.delivery, true, nil
}
func (store *workerStore) Complete(_ context.Context, _ int64, _ string, _ int, excerpt string, _ time.Time) error {
	store.completed, store.excerpt = true, excerpt
	return nil
}
func (store *workerStore) Fail(_ context.Context, _ int64, _ string, _ int, _ string, excerpt string, next time.Time, terminal bool) error {
	store.terminal, store.nextAttempt, store.excerpt = terminal, next, excerpt
	return nil
}
