package enrollment

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const lifetime = 10 * time.Minute

type Handler struct {
	agentToken []byte
	now        func() time.Time
	mu         sync.Mutex
	tickets    map[[32]byte]time.Time
}

func New(agentToken string, now func() time.Time) *Handler {
	return &Handler{agentToken: []byte(agentToken), now: now, tickets: make(map[[32]byte]time.Time)}
}

func (handler *Handler) Create(w http.ResponseWriter, _ *http.Request) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		writeError(w, http.StatusInternalServerError, "unable to create enrollment")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	hash := sha256.Sum256([]byte(token))
	expiresAt := handler.now().UTC().Add(lifetime)
	handler.mu.Lock()
	for ticket, expires := range handler.tickets {
		if !expires.After(handler.now()) {
			delete(handler.tickets, ticket)
		}
	}
	handler.tickets[hash] = expiresAt
	handler.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"enrollment_token": token, "expires_at": expiresAt})
}

func (handler *Handler) Redeem(w http.ResponseWriter, request *http.Request) {
	token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeError(w, http.StatusUnauthorized, "invalid enrollment credentials")
		return
	}
	hash := sha256.Sum256([]byte(token))
	handler.mu.Lock()
	expiresAt, found := handler.tickets[hash]
	if found {
		delete(handler.tickets, hash)
	}
	handler.mu.Unlock()
	if !found || !expiresAt.After(handler.now()) {
		writeError(w, http.StatusUnauthorized, "invalid enrollment credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agent_token": string(handler.agentToken)})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
