package overview

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestSummaryReturnsTotalsAndGapFreeSeries(t *testing.T) {
	pool := overviewTestDatabase(t)
	now := time.Date(2026, 7, 14, 12, 34, 20, 0, time.UTC)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO nodes (id, name, last_seen_at) VALUES ('flowlens-node-1', 'flowlens-node-1', $1)
	`, now.Add(-2*time.Second))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO traffic_minute (node_id, bucket, direction, bytes, packets) VALUES
			('flowlens-node-1', $1, 'inbound', 6000, 6),
			('flowlens-node-1', $1, 'outbound', 3000, 3),
			('flowlens-node-1', $2, 'inbound', 1200, 2)
	`, now.Truncate(time.Minute).Add(-20*time.Minute), now.Truncate(time.Minute).Add(-5*time.Minute))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO connection_details
		(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,bytes_sent,bytes_received,state)
		VALUES
		('c1','flowlens-node-1',$1,'outbound','tcp','10.0.0.2',42000,'203.0.113.10',443,'host','host','Host','api.example.test','confirmed',10,20,'established'),
		('c2','flowlens-node-1',$2,'outbound','tcp','10.0.0.2',42001,'203.0.113.11',443,'host','host','Host','203.0.113.11','ip_only',10,20,'established')
	`, now.Add(-2*time.Second), now.Add(-5*time.Second))
	require.NoError(t, err)
	service := NewService(pool, func() time.Time { return now })

	result, err := service.Summary(context.Background(), "flowlens-node-1", "1h")

	require.NoError(t, err)
	require.Equal(t, int64(7200), result.InboundBytes)
	require.Equal(t, int64(3000), result.OutboundBytes)
	require.Equal(t, int64(2), result.ActiveConnections)
	require.NotNil(t, result.DomainCoverage)
	require.Equal(t, 50.0, *result.DomainCoverage)
	require.Equal(t, now.Add(-2*time.Second), result.DataFreshAt)
	require.Len(t, result.Series, 60)
	require.Equal(t, int64(0), result.Series[0].InboundBytes)
	require.Equal(t, float64(100), pointAt(t, result.Series, now.Truncate(time.Minute).Add(-20*time.Minute)).InboundBPS)
	require.Equal(t, float64(50), pointAt(t, result.Series, now.Truncate(time.Minute).Add(-20*time.Minute)).OutboundBPS)
}

func TestSummaryRejectsUnknownNodeAndRange(t *testing.T) {
	pool := overviewTestDatabase(t)
	service := NewService(pool, func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) })

	_, err := service.Summary(context.Background(), "missing", "1h")
	require.ErrorIs(t, err, ErrNodeNotFound)
	_, err = service.Summary(context.Background(), "missing", "90d")
	require.ErrorIs(t, err, ErrInvalidRange)
}

func pointAt(t *testing.T, points []Point, at time.Time) Point {
	t.Helper()
	for _, point := range points {
		if point.At.Equal(at) {
			return point
		}
	}
	require.FailNow(t, "point not found", at.String())
	return Point{}
}

func overviewTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE collector_health, interface_deltas, traffic_minute, ingest_batches, nodes CASCADE")
	require.NoError(t, err)
	return pool
}
