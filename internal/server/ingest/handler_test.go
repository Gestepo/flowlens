package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestHandlerAcceptsAuthenticatedGzipBatch(t *testing.T) {
	store := &fakeBatchStore{inserted: true}
	handler := NewHandler("agent-secret", store)
	recorder := httptest.NewRecorder()
	request := batchRequest(t, validBatch())
	request.Header.Set("Authorization", "Bearer agent-secret")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "batch", store.batch.BatchID)
	require.JSONEq(t, `{"status":"accepted"}`, recorder.Body.String())
}

func TestHandlerReturnsOKForDuplicateBatch(t *testing.T) {
	store := &fakeBatchStore{inserted: false}
	handler := NewHandler("agent-secret", store)
	recorder := httptest.NewRecorder()
	request := batchRequest(t, validBatch())
	request.Header.Set("Authorization", "Bearer agent-secret")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"duplicate"}`, recorder.Body.String())
}

func TestHandlerRejectsBadBearerToken(t *testing.T) {
	store := &fakeBatchStore{inserted: true}
	handler := NewHandler("agent-secret", store)
	recorder := httptest.NewRecorder()
	request := batchRequest(t, validBatch())
	request.Header.Set("Authorization", "Bearer wrong-secret")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Zero(t, store.calls)
	require.JSONEq(t, `{"error":{"code":"unauthorized","message":"invalid agent credentials"}}`, recorder.Body.String())
}

func TestHandlerRejectsInvalidJSON(t *testing.T) {
	handler := NewHandler("agent-secret", &fakeBatchStore{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/batches", bytes.NewBufferString("{"))
	request.Header.Set("Authorization", "Bearer agent-secret")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"error":{"code":"invalid_json","message":"request body must contain one JSON batch"}}`, recorder.Body.String())
}

func TestHandlerReturnsInternalErrorWhenStoreFails(t *testing.T) {
	store := &fakeBatchStore{err: errors.New("database unavailable")}
	handler := NewHandler("agent-secret", store)
	recorder := httptest.NewRecorder()
	request := batchRequest(t, validBatch())
	request.Header.Set("Authorization", "Bearer agent-secret")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "database unavailable")
}

func TestHandlerRejectsCompressedBodyAboveTwoMiB(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	payload := make([]byte, 3<<20)
	_, err := random.Read(payload)
	require.NoError(t, err)
	var body bytes.Buffer
	writer := gzip.NewWriter(&body)
	_, err = writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Greater(t, body.Len(), 2<<20)

	handler := NewHandler("agent-secret", &fakeBatchStore{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/batches", &body)
	request.Header.Set("Authorization", "Bearer agent-secret")
	request.Header.Set("Content-Encoding", "gzip")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.JSONEq(t, `{"error":{"code":"request_too_large","message":"batch body exceeds the allowed size"}}`, recorder.Body.String())
}

func TestHandlerRejectsDecompressedBodyAboveSixteenMiB(t *testing.T) {
	batch := validBatch()
	batch.NodeID = strings.Repeat("n", (16<<20)+1)
	request := batchRequest(t, batch)
	request.Header.Set("Authorization", "Bearer agent-secret")
	recorder := httptest.NewRecorder()

	NewHandler("agent-secret", &fakeBatchStore{}).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestHandlerRejectsUnsupportedContentEncoding(t *testing.T) {
	body, err := json.Marshal(validBatch())
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/batches", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer agent-secret")
	request.Header.Set("Content-Encoding", "br")
	recorder := httptest.NewRecorder()

	NewHandler("agent-secret", &fakeBatchStore{inserted: true}).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
}

type fakeBatchStore struct {
	inserted bool
	err      error
	calls    int
	batch    model.Batch
}

func (s *fakeBatchStore) InsertBatch(_ context.Context, batch model.Batch) (bool, error) {
	s.calls++
	s.batch = batch
	return s.inserted, s.err
}

func batchRequest(t *testing.T, batch model.Batch) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := gzip.NewWriter(&body)
	require.NoError(t, json.NewEncoder(writer).Encode(batch))
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/batches", &body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	return request
}

func validBatch() model.Batch {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	return model.Batch{
		SchemaVersion: 1,
		BatchID:       "batch",
		NodeID:        "flowlens-node-1",
		SentAt:        now,
		Events: []model.Event{{
			ID:         "event",
			ObservedAt: now,
			Kind:       model.EventInterfaceDelta,
			Direction:  model.DirectionInbound,
			Bytes:      1024,
			Packets:    2,
			Interface:  "enp0s6",
		}},
	}
}
