package migrations_test

import (
	"context"
	"os"
	"testing"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestApplyIsIdempotent(t *testing.T) {
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, migrations.Apply(context.Background(), pool))
	require.NoError(t, migrations.Apply(context.Background(), pool))

	for _, expected := range []string{"traffic_minute", "owners", "domain_evidence", "connection_details", "proxy_request_details", "owner_minute", "domain_minute", "flow_minute", "proxy_status_minute"} {
		var table string
		require.NoError(t, pool.QueryRow(context.Background(), "SELECT to_regclass('public.' || $1)::text", expected).Scan(&table))
		require.Equal(t, expected, table)
	}
	var databasePressureKind string
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT kind FROM alert_rules WHERE id='database-pressure'").Scan(&databasePressureKind))
	require.Equal(t, "database_pressure", databasePressureKind)
	var usageColumn string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT column_name FROM information_schema.columns WHERE table_name='collector_health' AND column_name='usage_percent'`).Scan(&usageColumn))
	require.Equal(t, "usage_percent", usageColumn)
	var requestsColumn string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT column_name FROM information_schema.columns WHERE table_name='flow_minute' AND column_name='requests'`).Scan(&requestsColumn))
	require.Equal(t, "requests", requestsColumn)
}
