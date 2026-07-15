package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("authentication record not found")

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Session struct {
	TokenHash     [32]byte
	AdminID       int64
	Username      string
	CSRFToken     string
	CreatedAt     time.Time
	LastSeenAt    time.Time
	IdleExpiresAt time.Time
	ExpiresAt     time.Time
}

type Store interface {
	AdminCount(context.Context) (int, error)
	CreateAdmin(context.Context, string, string) (Admin, error)
	FindAdmin(context.Context, string) (Admin, error)
	UpdatePassword(context.Context, int64, string) error
	CreateSession(context.Context, Session) error
	FindSession(context.Context, [32]byte) (Session, error)
	TouchSession(context.Context, [32]byte, time.Time, time.Time) error
	DeleteSession(context.Context, [32]byte) error
	DeleteAdminSessions(context.Context, int64) error
}

type Handler struct {
	store          Store
	bootstrapToken string
	now            func() time.Time
	limiter        *LoginLimiter
	dummyHash      string
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type sessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username"`
	CSRFToken     string `json:"csrf_token"`
}

func NewHandler(store Store, bootstrapToken string, now func() time.Time) *Handler {
	dummyHash, _ := Hash("flowlens timing placeholder", DefaultParams)
	return &Handler{store: store, bootstrapToken: bootstrapToken, now: now, limiter: NewLoginLimiter(now), dummyHash: dummyHash}
}

func (handler *Handler) Bootstrap(w http.ResponseWriter, request *http.Request) {
	if !bearerMatches(request.Header.Get("Authorization"), handler.bootstrapToken) {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "初始化凭证无效")
		return
	}
	count, err := handler.store.AdminCount(request.Context())
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "无法读取管理员状态")
		return
	}
	if count != 0 {
		writeAuthError(w, http.StatusConflict, "already_initialized", "管理员已经初始化")
		return
	}
	var input credentials
	if !decodeJSON(w, request, &input) {
		return
	}
	input.Username = normalizeUsername(input.Username)
	if input.Username == "" || len(input.Username) > 128 {
		writeAuthError(w, http.StatusBadRequest, "invalid_username", "用户名不能为空且最多 128 个字符")
		return
	}
	hash, err := Hash(input.Password, DefaultParams)
	if err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid_password", err.Error())
		return
	}
	admin, err := handler.store.CreateAdmin(request.Context(), input.Username, hash)
	if err != nil {
		writeAuthError(w, http.StatusConflict, "already_initialized", "管理员已经初始化")
		return
	}
	handler.startSession(w, request, admin, http.StatusCreated)
}

func (handler *Handler) Login(w http.ResponseWriter, request *http.Request) {
	var input credentials
	if !decodeJSON(w, request, &input) {
		return
	}
	input.Username = normalizeUsername(input.Username)
	ip := clientIP(request)
	if handler.limiter.Blocked(ip, input.Username) {
		handler.writeRateLimit(w, ip, input.Username)
		return
	}
	admin, err := handler.store.FindAdmin(request.Context(), input.Username)
	encoded := handler.dummyHash
	if err == nil {
		encoded = admin.PasswordHash
	}
	match, needsRehash := Verify(input.Password, encoded, DefaultParams)
	if err != nil || !match {
		handler.limiter.Failed(ip, input.Username)
		if handler.limiter.Blocked(ip, input.Username) {
			handler.writeRateLimit(w, ip, input.Username)
			return
		}
		writeAuthError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码不正确")
		return
	}
	if needsRehash {
		if hash, hashErr := Hash(input.Password, DefaultParams); hashErr == nil {
			_ = handler.store.UpdatePassword(request.Context(), admin.ID, hash)
		}
	}
	handler.limiter.Succeeded(ip, input.Username)
	handler.startSession(w, request, admin, http.StatusOK)
}

func (handler *Handler) Logout(w http.ResponseWriter, request *http.Request) {
	session, ok := SessionFromContext(request.Context())
	if ok {
		_ = handler.store.DeleteSession(request.Context(), session.TokenHash)
	}
	http.SetCookie(w, expiredSessionCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) Password(w http.ResponseWriter, request *http.Request) {
	session, ok := SessionFromContext(request.Context())
	if !ok {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "需要登录")
		return
	}
	var input passwordChange
	if !decodeJSON(w, request, &input) {
		return
	}
	admin, err := handler.store.FindAdmin(request.Context(), session.Username)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "需要重新登录")
		return
	}
	match, _ := Verify(input.CurrentPassword, admin.PasswordHash, DefaultParams)
	if !match {
		writeAuthError(w, http.StatusUnauthorized, "invalid_credentials", "当前密码不正确")
		return
	}
	hash, err := Hash(input.NewPassword, DefaultParams)
	if err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid_password", err.Error())
		return
	}
	if err := handler.store.UpdatePassword(request.Context(), admin.ID, hash); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "无法更新密码")
		return
	}
	if err := handler.store.DeleteAdminSessions(request.Context(), admin.ID); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "无法轮换会话")
		return
	}
	handler.startSession(w, request, admin, http.StatusOK)
}

func (handler *Handler) Session(w http.ResponseWriter, request *http.Request) {
	session, ok := SessionFromContext(request.Context())
	if !ok {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "需要登录")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, Username: session.Username, CSRFToken: session.CSRFToken})
}

func (handler *Handler) startSession(w http.ResponseWriter, request *http.Request, admin Admin, status int) {
	now := handler.now()
	material, err := newSessionMaterial(now)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "无法创建会话")
		return
	}
	session := Session{TokenHash: material.TokenHash, AdminID: admin.ID, Username: admin.Username, CSRFToken: material.CSRFToken, CreatedAt: now, LastSeenAt: now, IdleExpiresAt: material.IdleExpiresAt, ExpiresAt: material.ExpiresAt}
	if err := handler.store.CreateSession(request.Context(), session); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "无法保存会话")
		return
	}
	http.SetCookie(w, material.Cookie())
	writeJSON(w, status, sessionResponse{Authenticated: true, Username: admin.Username, CSRFToken: material.CSRFToken})
}

func (handler *Handler) writeRateLimit(w http.ResponseWriter, ip, username string) {
	retry := handler.limiter.RetryAfter(ip, username)
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Round(time.Second).Seconds())))
	writeAuthError(w, http.StatusTooManyRequests, "rate_limited", "登录尝试过多，请稍后再试")
}

func decodeJSON(w http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(w, request.Body, 64<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return false
	}
	return true
}

func bearerMatches(header, expected string) bool {
	provided := strings.TrimPrefix(header, "Bearer ")
	return expected != "" && len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func tokenHash(token string) [32]byte { return sha256.Sum256([]byte(token)) }
