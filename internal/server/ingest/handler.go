package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"flowlens/internal/model"
)

const (
	maxCompressedBytes   = 2 << 20
	maxDecompressedBytes = 16 << 20
)

var (
	errRequestTooLarge     = errors.New("request body too large")
	errUnsupportedEncoding = errors.New("unsupported content encoding")
)

type BatchStore interface {
	InsertBatch(context.Context, model.Batch) (bool, error)
}

type Handler struct {
	token []byte
	store BatchStore
}

func NewHandler(token string, store BatchStore) *Handler {
	return &Handler{token: []byte(token), store: store}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method must be POST")
		return
	}
	if !h.authorized(r.Header.Get("Authorization")) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid agent credentials")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCompressedBytes)
	batch, err := decodeBatch(r)
	if err != nil {
		if errors.Is(err, errRequestTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "batch body exceeds the allowed size")
			return
		}
		if errors.Is(err, errUnsupportedEncoding) {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_encoding", "content encoding must be gzip or identity")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON batch")
		return
	}
	if err := batch.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_batch", err.Error())
		return
	}

	inserted, err := h.store.InsertBatch(r.Context(), batch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "unable to store batch")
		return
	}
	if !inserted {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) authorized(header string) bool {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || len(token) != len(h.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), h.token) == 1
}

func decodeBatch(r *http.Request) (model.Batch, error) {
	var reader io.Reader = r.Body
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gzipReader, err := gzip.NewReader(r.Body)
		if err != nil {
			return model.Batch{}, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	default:
		return model.Batch{}, errUnsupportedEncoding
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxDecompressedBytes+1))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return model.Batch{}, errRequestTooLarge
		}
		return model.Batch{}, err
	}
	if len(body) > maxDecompressedBytes {
		return model.Batch{}, errRequestTooLarge
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	var batch model.Batch
	if err := decoder.Decode(&batch); err != nil {
		return model.Batch{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return model.Batch{}, errors.New("request contains trailing JSON")
	}
	return batch, nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
