package operations

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresServiceReturnsOperationalStateAndUpdatesSettings(t *testing.T) {
	pool := operationsTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	var databaseBytes int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&databaseBytes))
	service := NewPostgresService(pool, databaseBytes)

	nodes, err := service.ListNodes(ctx, now)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "partial", nodes[0].Status)
	require.Equal(t, []string{"npm"}, nodes[0].FailedCollectors)

	health, err := service.Health(ctx, now)
	require.NoError(t, err)
	require.Equal(t, "partial", health.Status)
	require.InDelta(t, 100, health.DatabaseUsagePercent, 2)
	require.Equal(t, 1, health.WebhookTerminalFailures)
	require.Equal(t, "failed", health.Jobs["retention"])

	alerts, err := service.ListAlerts(ctx, "open", 50, 0)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
	alert, err := service.GetAlert(ctx, alerts[0].ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"node": "node-a", "ip": "1.1.1.1"}, alert.TrafficFilter)
	require.Len(t, alert.Deliveries, 1)

	settings, err := service.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 30, settings.DetailRetentionDays)
	require.NotEmpty(t, settings.AlertRules)
	require.NoError(t, service.UpdateRetention(ctx, 14, 6))
	require.NoError(t, service.RenameNode(ctx, "node-a", "Edge VPS"))
	settings, err = service.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 14, settings.DetailRetentionDays)
	var name string
	require.NoError(t, pool.QueryRow(ctx, "SELECT name FROM nodes WHERE id='node-a'").Scan(&name))
	require.Equal(t, "Edge VPS", name)
}

func operationsTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE webhook_deliveries, alerts, alert_rules, collector_health, job_leases, nodes CASCADE")
	require.NoError(t, err)
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	_, err = pool.Exec(context.Background(), `INSERT INTO nodes(id,name,last_seen_at) VALUES ('node-a','Main', $1)`, now.Add(-time.Second))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO collector_health(event_id,node_id,observed_at,collector,status,code) VALUES ('health-1','node-a',$1,'npm','failed','unreadable')`, now.Add(-time.Second))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO alert_rules(id,kind,name,severity,config) VALUES ('rate','absolute_rate','速率过高','warning','{"threshold":100}')`)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO alerts(rule_id,fingerprint,severity,status,node_id,title,evidence,observed_value,comparison_value,window_seconds,first_seen_at,last_seen_at)
		VALUES ('rate','fingerprint','warning','open','node-a','速率过高','{"node_id":"node-a","destination":"1.1.1.1"}',120,100,300,$1,$1) RETURNING id
	`, now)
	require.NoError(t, err)
	var alertID int64
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT id FROM alerts WHERE fingerprint='fingerprint'").Scan(&alertID))
	_, err = pool.Exec(context.Background(), `INSERT INTO webhook_deliveries(alert_id,event_type,status,attempt,next_attempt_at,created_at,last_error) VALUES ($1,'opened','terminal',6,$2,$2,'failed')`, alertID, now)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO job_leases(name,next_run_at,last_error) VALUES ('retention',$1,'failed')`, now)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), "UPDATE operation_settings SET detail_retention_days=30,aggregate_retention_months=12 WHERE id=1")
	require.NoError(t, err)
	return pool
}
