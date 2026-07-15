package sender

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestSenderPostsAuthenticatedGzipBatch(t *testing.T) {
	var received model.Batch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer agent-secret", r.Header.Get("Authorization"))
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		reader, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.NewDecoder(reader).Decode(&received))
		require.NoError(t, reader.Close())
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	sender, err := New(server.URL, "agent-secret")
	require.NoError(t, err)

	err = sender.Send(context.Background(), senderBatch())

	require.NoError(t, err)
	require.Equal(t, "batch", received.BatchID)
}

func TestSenderRetriesServerErrorThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var delays []time.Duration
	sender, err := New(server.URL, "token",
		WithBackoff([]time.Duration{time.Second}),
		WithSleep(func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil }),
	)
	require.NoError(t, err)

	err = sender.Send(context.Background(), senderBatch())

	require.NoError(t, err)
	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, []time.Duration{time.Second}, delays)
}

func TestSenderDoesNotRetryClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	sender, err := New(server.URL, "token", WithBackoff([]time.Duration{time.Millisecond}))
	require.NoError(t, err)

	err = sender.Send(context.Background(), senderBatch())

	require.ErrorIs(t, err, ErrPermanent)
	require.Equal(t, int32(1), attempts.Load())
}

func TestSenderStopsRetryWhenContextIsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	sender, err := New(server.URL, "token",
		WithBackoff([]time.Duration{time.Hour}),
		WithSleep(func(ctx context.Context, _ time.Duration) error {
			cancel()
			<-ctx.Done()
			return ctx.Err()
		}),
	)
	require.NoError(t, err)

	err = sender.Send(ctx, senderBatch())

	require.True(t, errors.Is(err, context.Canceled))
}

func TestSenderRetriesNetworkError(t *testing.T) {
	var attempts atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("temporary network failure")
		}
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(http.NoBody)}, nil
	})}
	sender, err := New("https://flowlens.invalid/api/v1/agent/batches", "token",
		WithHTTPClient(client),
		WithBackoff([]time.Duration{time.Millisecond}),
		WithSleep(func(context.Context, time.Duration) error { return nil }),
	)
	require.NoError(t, err)

	err = sender.Send(context.Background(), senderBatch())

	require.NoError(t, err)
	require.Equal(t, int32(2), attempts.Load())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func senderBatch() model.Batch {
	now := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)
	return model.Batch{
		SchemaVersion: 1,
		BatchID:       "batch",
		NodeID:        "flowlens-node-1",
		SentAt:        now,
		Events: []model.Event{{
			ID:         "event",
			ObservedAt: now,
			Kind:       model.EventInterfaceDelta,
			Direction:  model.DirectionOutbound,
			Bytes:      100,
			Interface:  "enp0s6",
		}},
	}
}
