package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
)

type sessionContextKey struct{}

func (handler *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "需要登录")
			return
		}
		hash := tokenHash(cookie.Value)
		session, err := handler.store.FindSession(request.Context(), hash)
		now := handler.now()
		if err != nil || !now.Before(session.ExpiresAt) || !now.Before(session.IdleExpiresAt) {
			if !errors.Is(err, ErrNotFound) {
				_ = handler.store.DeleteSession(request.Context(), hash)
			}
			http.SetCookie(w, expiredSessionCookie())
			writeAuthError(w, http.StatusUnauthorized, "session_expired", "会话已过期")
			return
		}
		if unsafeMethod(request.Method) {
			provided := request.Header.Get("X-CSRF-Token")
			if len(provided) != len(session.CSRFToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
				writeAuthError(w, http.StatusForbidden, "csrf_rejected", "请求验证失败")
				return
			}
		}
		session.LastSeenAt = now
		session.IdleExpiresAt = now.Add(idleLifetime)
		if err := handler.store.TouchSession(request.Context(), hash, session.LastSeenAt, session.IdleExpiresAt); err != nil {
			writeAuthError(w, http.StatusUnauthorized, "session_expired", "会话已失效")
			return
		}
		ctx := context.WithValue(request.Context(), sessionContextKey{}, session)
		next.ServeHTTP(w, request.WithContext(ctx))
	})
}

func SessionFromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(Session)
	return session, ok
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
