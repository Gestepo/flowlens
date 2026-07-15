package rollup

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestRollupRangeRecomputesHourAndDayBucketsIdempotently(t *testing.T) {
	pool := rollupTestDatabase(t)
	ctx := context.Background()
	service := NewService(pool)
	start := time.Date(2026, 7, 14, 23, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	require.NoError(t, service.RollupRange(ctx, ResolutionHour, start, end))
	first := rollupSnapshot(t, pool, ResolutionHour)
	require.NotEmpty(t, first)
	var flowRequests int64
	require.NoError(t, pool.QueryRow(ctx, "SELECT coalesce(sum(requests),0) FROM traffic_rollups WHERE resolution='hour' AND dimension_kind='flow'").Scan(&flowRequests))
	require.Equal(t, int64(3), flowRequests)
	require.NoError(t, service.RollupRange(ctx, ResolutionHour, start, end))
	require.Equal(t, first, rollupSnapshot(t, pool, ResolutionHour))

	require.NoError(t, service.RollupRange(ctx, ResolutionDay, start.Truncate(24*time.Hour), start.Truncate(24*time.Hour).Add(48*time.Hour)))
	var dayBytes int64
	require.NoError(t, pool.QueryRow(ctx, "SELECT coalesce(sum(bytes),0) FROM traffic_rollups WHERE resolution='day' AND dimension_kind='traffic'").Scan(&dayBytes))
	require.Equal(t, int64(600), dayBytes)
}

func rollupTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE traffic_rollups, proxy_status_minute, flow_minute, domain_minute, owner_minute, traffic_minute, nodes CASCADE")
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO nodes(id,name,last_seen_at) VALUES ('node-a','node-a',now());
		INSERT INTO traffic_minute(node_id,bucket,direction,bytes,packets) VALUES
		('node-a','2026-07-14 23:10:00+00','inbound',100,1),('node-a','2026-07-14 23:50:00+00','inbound',200,2),('node-a','2026-07-15 00:10:00+00','outbound',300,3);
		INSERT INTO owner_minute(node_id,bucket,owner_id,owner_kind,owner_name,direction,bytes,connections) VALUES
		('node-a','2026-07-14 23:10:00+00','container:web','container','web','outbound',75,2);
		INSERT INTO domain_minute(node_id,bucket,direction,domain,confidence,bytes,connections,requests) VALUES
		('node-a','2026-07-14 23:10:00+00','outbound','example.com','confirmed',50,1,0);
		INSERT INTO flow_minute(node_id,bucket,direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,bytes,connections,requests) VALUES
		('node-a','2026-07-14 23:10:00+00','outbound','container:web','web','10.0.0.2','1.1.1.1','example.com','confirmed','tcp',443,40,1,0),
		('node-a','2026-07-14 23:10:00+00','inbound','container:web','web','198.51.100.1','web','app.example.com','confirmed','tcp',8080,25,0,3);
		INSERT INTO proxy_status_minute(node_id,bucket,host,status,bytes,requests) VALUES
		('node-a','2026-07-14 23:10:00+00','app.example.com',200,25,3);
	`)
	require.NoError(t, err)
	return pool
}

func rollupSnapshot(t *testing.T, pool *pgxpool.Pool, resolution string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT node_id||'|'||bucket::text||'|'||dimension_kind||'|'||dimension_key||'|'||direction||'|'||status_code||'|'||bytes||'|'||packets||'|'||connections||'|'||requests
		FROM traffic_rollups WHERE resolution=$1 ORDER BY 1
	`, resolution)
	require.NoError(t, err)
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	return values
}
