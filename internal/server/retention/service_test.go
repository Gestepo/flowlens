package retention

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCleanupPreservesBoundariesAndRequiresDayCoverage(t *testing.T) {
	pool := retentionTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	service := NewService(pool)
	stats, err := service.Cleanup(ctx, now, Settings{DetailDays: 30, AggregateMonths: 12})
	require.NoError(t, err)
	require.Greater(t, stats.DeletedRows, int64(0))

	var detailIDs []string
	rows, err := pool.Query(ctx, "SELECT event_id FROM connection_details ORDER BY event_id")
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		detailIDs = append(detailIDs, id)
	}
	rows.Close()
	require.Equal(t, []string{"age-29", "age-30", "age-31-no-rollup"}, detailIDs)

	var dayBuckets, alerts int64
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM traffic_rollups WHERE resolution='day'").Scan(&dayBuckets))
	require.Equal(t, int64(1), dayBuckets)
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM alerts").Scan(&alerts))
	require.Equal(t, int64(2), alerts)
}

func retentionTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE webhook_deliveries, alerts, alert_rules, traffic_rollups, connection_details, nodes CASCADE")
	require.NoError(t, err)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	insertDetail := func(id string, at time.Time) {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO connection_details(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,bytes_sent,bytes_received)
			VALUES ($1,'node-a',$2,'outbound','tcp','10.0.0.2',40000,'1.1.1.1',443,'host:test','host','test','1.1.1.1','ip_only',10,10)
		`, id, at)
		require.NoError(t, err)
	}
	_, err = pool.Exec(context.Background(), "INSERT INTO nodes(id,name,last_seen_at) VALUES ('node-a','node-a',now())")
	require.NoError(t, err)
	insertDetail("age-29", now.AddDate(0, 0, -29))
	insertDetail("age-30", now.AddDate(0, 0, -30))
	insertDetail("age-31-covered", now.AddDate(0, 0, -31))
	insertDetail("age-31-no-rollup", now.AddDate(0, 0, -32))
	coveredDay := now.AddDate(0, 0, -31).Truncate(24 * time.Hour)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO traffic_rollups(resolution,node_id,bucket,dimension_kind,dimension_key,direction,status_code,bytes,source_min_at,source_max_at)
		VALUES ('day','node-a',$1,'traffic','total','outbound',0,20,$1,$1),
		('day','node-a',$2,'traffic','total','outbound',0,20,$2,$2)
	`, coveredDay, now.AddDate(-1, 0, -1).Truncate(24*time.Hour))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), "INSERT INTO alert_rules(id,kind,name,severity) VALUES ('rate','absolute_rate','rate','warning')")
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO alerts(rule_id,fingerprint,severity,status,node_id,title,observed_value,window_seconds,first_seen_at,last_seen_at,resolved_at) VALUES
		('rate','old-resolved','warning','resolved','node-a','old',1,300,$1,$1,$1),
		('rate','recent-resolved','warning','resolved','node-a','recent',1,300,$2,$2,$2),
		('rate','old-open','warning','open','node-a','open',1,300,$1,$1,NULL)
	`, now.AddDate(-1, 0, -1), now.AddDate(0, -11, 0))
	require.NoError(t, err)
	return pool
}
