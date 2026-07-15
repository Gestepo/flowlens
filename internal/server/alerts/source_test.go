package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPostgresSourceBuildsRateAndCoverageObservations(t *testing.T) {
	pool := alertTestDatabase(t)
	now := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)
	_, err := pool.Exec(context.Background(), `INSERT INTO traffic_minute(node_id,bucket,direction,bytes,packets) VALUES ('node-a',$1,'outbound',600,1)`, now.Add(-time.Minute))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO flow_minute(node_id,bucket,direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,bytes,connections) VALUES
		('node-a',$1,'outbound','host:test','test','10.0.0.2','1.1.1.1','example.com','confirmed','tcp',443,100,1),
		('node-a',$1,'outbound','host:test','test','10.0.0.2','2.2.2.2','','ip_only','tcp',443,100,1)
	`, now.Add(-time.Minute))
	require.NoError(t, err)
	source := NewPostgresSource(pool)
	rate, err := source.Observations(context.Background(), Rule{Kind: KindAbsoluteRate}, "node-a", now)
	require.NoError(t, err)
	require.Equal(t, float64(2), rate[0].RateBPS)
	coverage, err := source.Observations(context.Background(), Rule{Kind: KindDomainCoverage}, "node-a", now)
	require.NoError(t, err)
	require.Equal(t, float64(50), coverage[0].DomainCoverage)
}

func TestPostgresSourceBuildsDatabaseAndBufferPressureObservations(t *testing.T) {
	pool := alertTestDatabase(t)
	now := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)
	_, err := pool.Exec(context.Background(), `INSERT INTO collector_health(event_id,node_id,observed_at,collector,status,code,message,usage_percent) VALUES
		('pressure','node-a',$1,'spool','degraded','buffer_pressure','buffer is 82 percent full',82),
		('recovered','node-a',$2,'spool','healthy','buffer_recovered','buffer recovered',0)`, now.Add(-time.Minute), now.Add(-30*time.Second))
	require.NoError(t, err)
	var databaseBytes int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT pg_database_size(current_database())`).Scan(&databaseBytes))
	source := NewPostgresSource(pool, WithDatabaseBudgetBytes(databaseBytes))
	database, err := source.Observations(context.Background(), Rule{Kind: KindDatabasePressure}, "node-a", now)
	require.NoError(t, err)
	require.InDelta(t, 100, database[0].DatabaseUsagePercent, 2)
	buffer, err := source.Observations(context.Background(), Rule{Kind: KindBufferPressure}, "node-a", now)
	require.NoError(t, err)
	require.Equal(t, float64(82), buffer[0].BufferUsagePercent)
}
