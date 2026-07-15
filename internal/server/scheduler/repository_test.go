package scheduler

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresStorePersistsNextRunAndReclaimsExpiredLease(t *testing.T) {
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE job_leases")
	require.NoError(t, err)
	store := NewPostgresStore(pool)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	claimed, err := store.Claim(context.Background(), "rollup", "worker-a", now, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = store.Claim(context.Background(), "rollup", "worker-b", now, 2*time.Minute)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, store.Finish(context.Background(), "rollup", "worker-a", true, now, now.Add(time.Hour), ""))

	restarted := NewPostgresStore(pool)
	claimed, err = restarted.Claim(context.Background(), "rollup", "worker-b", now.Add(30*time.Minute), 2*time.Minute)
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = restarted.Claim(context.Background(), "rollup", "worker-b", now.Add(time.Hour), 2*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
}
