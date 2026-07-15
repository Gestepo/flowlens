package auth

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresStorePersistsOnlySessionTokenHash(t *testing.T) {
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(ctx, pool))
	_, err = pool.Exec(ctx, "TRUNCATE browser_sessions, administrators CASCADE")
	require.NoError(t, err)

	store := NewPostgresStore(pool)
	admin, err := store.CreateAdmin(ctx, "admin", "encoded-password-hash")
	require.NoError(t, err)
	raw := "raw-browser-cookie-token"
	hash := sha256.Sum256([]byte(raw))
	now := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	require.NoError(t, store.CreateSession(ctx, Session{
		TokenHash: hash, AdminID: admin.ID, Username: admin.Username, CSRFToken: "csrf-token-value-with-more-than-32-characters",
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(30 * time.Minute), ExpiresAt: now.Add(24 * time.Hour),
	}))

	loaded, err := store.FindSession(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, "admin", loaded.Username)
	require.Equal(t, hash, loaded.TokenHash)
	var rawStored bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM browser_sessions WHERE token_hash = convert_to($1, 'UTF8'))", raw).Scan(&rawStored))
	require.False(t, rawStored)
}
