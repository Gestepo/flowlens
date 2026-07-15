package webhook

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestRepositoryClaimsOnceAndReclaimsExpiredLease(t *testing.T) {
	pool := webhookTestDatabase(t)
	repository := NewRepository(pool)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)

	delivery, ok, err := repository.Claim(context.Background(), "worker-a", now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(42), delivery.AlertID)
	require.Equal(t, "delivery-1", delivery.PublicID)

	_, ok, err = repository.Claim(context.Background(), "worker-b", now)
	require.NoError(t, err)
	require.False(t, ok)

	_, err = pool.Exec(context.Background(), "UPDATE webhook_deliveries SET lease_expires_at=$1 WHERE id=$2", now.Add(-time.Second), delivery.ID)
	require.NoError(t, err)
	reclaimed, ok, err := repository.Claim(context.Background(), "worker-b", now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, delivery.ID, reclaimed.ID)
	require.NoError(t, repository.Complete(context.Background(), reclaimed.ID, "worker-b", 204, "ok", now))

	var status string
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT status FROM webhook_deliveries WHERE id=$1", delivery.ID).Scan(&status))
	require.Equal(t, "delivered", status)
}

func TestRepositoryPersistsRetryAndTerminalState(t *testing.T) {
	pool := webhookTestDatabase(t)
	repository := NewRepository(pool)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	delivery, ok, err := repository.Claim(context.Background(), "worker-a", now)
	require.NoError(t, err)
	require.True(t, ok)
	next := now.Add(time.Minute)
	require.NoError(t, repository.Fail(context.Background(), delivery.ID, "worker-a", 503, "unavailable", "body", next, false))

	var status string
	var attempt int
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT status,attempt FROM webhook_deliveries WHERE id=$1", delivery.ID).Scan(&status, &attempt))
	require.Equal(t, "pending", status)
	require.Equal(t, 1, attempt)

	_, err = pool.Exec(context.Background(), "UPDATE webhook_deliveries SET status='leased',lease_owner='worker-a',lease_expires_at=$2 WHERE id=$1", delivery.ID, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.NoError(t, repository.Fail(context.Background(), delivery.ID, "worker-a", 400, "bad request", "body", time.Time{}, true))
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT status,attempt FROM webhook_deliveries WHERE id=$1", delivery.ID).Scan(&status, &attempt))
	require.Equal(t, "terminal", status)
	require.Equal(t, 2, attempt)
}

func TestRepositoryStoresSettingsAndQueuesTestPayload(t *testing.T) {
	pool := webhookTestDatabase(t)
	repository := NewRepository(pool)
	ctx := context.Background()
	settings := WebhookSettings{Enabled: true, Endpoint: "https://hooks.example.test/flowlens", EncryptedSecret: []byte("encrypted-value"), UpdatedAt: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)}
	require.NoError(t, repository.SaveSettings(ctx, settings))
	loaded, err := repository.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, settings.Endpoint, loaded.Endpoint)
	require.Equal(t, settings.EncryptedSecret, loaded.EncryptedSecret)

	_, err = pool.Exec(ctx, "TRUNCATE webhook_deliveries")
	require.NoError(t, err)
	id, err := repository.QueueTest(ctx)
	require.NoError(t, err)
	require.NotZero(t, id)
	delivery, ok, err := repository.Claim(ctx, "worker-a", time.Now().UTC().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "test", delivery.EventType)
	require.Equal(t, "Webhook 测试", delivery.Title)
	require.Equal(t, "settings", delivery.Evidence["source"])

	_, err = pool.Exec(ctx, "UPDATE webhook_deliveries SET status='pending',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1", id)
	require.NoError(t, err)
	settings.Endpoint = "https://new-hooks.example.test/flowlens"
	require.NoError(t, repository.SaveSettings(ctx, settings))
	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM webhook_deliveries WHERE id=$1", id).Scan(&status))
	require.Equal(t, "cancelled", status)
}

func webhookTestDatabase(t *testing.T) *pgxpool.Pool {
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
	_, err = pool.Exec(context.Background(), "UPDATE webhook_settings SET enabled=false,endpoint='',encrypted_secret=NULL")
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO nodes(id,name,last_seen_at) VALUES ('node-a','node-a',now());
		INSERT INTO alert_rules(id,kind,name,severity) VALUES ('rate','absolute_rate','速率过高','warning');
		INSERT INTO alerts(id,rule_id,fingerprint,severity,status,node_id,title,observed_value,window_seconds,first_seen_at,last_seen_at)
		VALUES (42,'rate','fingerprint','warning','open','node-a','速率过高',20,300,now(),now());
		INSERT INTO webhook_deliveries(id,alert_id,event_type,status,next_attempt_at,created_at) VALUES (1,42,'opened','pending','2026-07-15 07:00:00+00','2026-07-15 07:00:00+00');
		SELECT setval(pg_get_serial_sequence('webhook_deliveries','id'), 1, true);
	`)
	require.NoError(t, err)
	return pool
}
