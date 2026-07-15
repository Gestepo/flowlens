package alerts

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestRepositoryReconcilesAlertLifecycleAndDelivery(t *testing.T) {
	pool := alertTestDatabase(t)
	ctx := context.Background()
	repository := NewRepository(pool)
	now := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	rule := Rule{ID: "rate", Kind: KindAbsoluteRate, Name: "速率过高", Enabled: true, Severity: SeverityWarning, Threshold: 10, WindowSeconds: 300}
	finding := EvaluateRule(rule, Observation{NodeID: "node-a", RateBPS: 20}, now)[0]

	require.NoError(t, repository.Reconcile(ctx, rule, "node-a", []Finding{finding}, now))
	require.NoError(t, repository.Reconcile(ctx, rule, "node-a", []Finding{finding}, now.Add(time.Minute)))
	require.Equal(t, int64(1), countRows(t, pool, "alerts"))
	require.Equal(t, int64(1), countRows(t, pool, "webhook_deliveries"))
	var occurrences int64
	require.NoError(t, pool.QueryRow(ctx, "SELECT occurrence_count FROM alerts WHERE status='open'").Scan(&occurrences))
	require.Equal(t, int64(2), occurrences)

	require.NoError(t, repository.Reconcile(ctx, rule, "node-a", nil, now.Add(2*time.Minute)))
	require.NoError(t, repository.Reconcile(ctx, rule, "node-a", nil, now.Add(3*time.Minute)))
	require.Equal(t, int64(0), countWhere(t, pool, "alerts", "status='open'"))

	recurrence := EvaluateRule(rule, Observation{NodeID: "node-a", RateBPS: 20}, now.Add(4*time.Minute))[0]
	require.NoError(t, repository.Reconcile(ctx, rule, "node-a", []Finding{recurrence}, now.Add(4*time.Minute)))
	require.Equal(t, int64(2), countRows(t, pool, "alerts"))
	require.Equal(t, int64(2), countRows(t, pool, "webhook_deliveries"))
}

func TestRepositorySerializesConcurrentEvaluators(t *testing.T) {
	pool := alertTestDatabase(t)
	ctx := context.Background()
	repository := NewRepository(pool)
	now := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	rule := Rule{ID: "stale", Kind: KindAgentStale, Name: "节点离线", Enabled: true, Severity: SeverityCritical, Threshold: 60, WindowSeconds: 300}
	finding := EvaluateRule(rule, Observation{NodeID: "node-a", AgentAgeSeconds: 90}, now)[0]

	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			errors <- repository.Reconcile(ctx, rule, "node-a", []Finding{finding}, now)
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), countWhere(t, pool, "alerts", "status='open'"))
	require.Equal(t, int64(1), countRows(t, pool, "webhook_deliveries"))
}

func alertTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE webhook_deliveries, alerts, alert_rules, nodes CASCADE")
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), "INSERT INTO nodes(id,name,last_seen_at) VALUES ('node-a','node-a',now())")
	require.NoError(t, err)
	return pool
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int64 {
	return countWhere(t, pool, table, "true")
}
func countWhere(t *testing.T, pool *pgxpool.Pool, table, clause string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE "+clause).Scan(&count))
	return count
}
