package migrations_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestDetailTablesRouteCurrentDataToMonthlyPartitions(t *testing.T) {
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(ctx, pool))
	_, err = pool.Exec(ctx, "TRUNCATE connection_details, proxy_request_details, nodes CASCADE")
	require.NoError(t, err)
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, "INSERT INTO nodes(id,name,last_seen_at) VALUES ('partition-node','partition-node',$1)", now)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO interface_deltas(event_id,node_id,observed_at,interface,direction,bytes,packets)
		VALUES ('interface','partition-node',$1,'eth0','inbound',1,1)`, now)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO connection_details(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,bytes_sent,bytes_received)
		VALUES ('connection','partition-node',$1,'outbound','tcp','10.0.0.2',40000,'1.1.1.1',443,'host','host','Host','1.1.1.1','ip_only',1,1)`, now)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO proxy_request_details(event_id,node_id,observed_at,host,source_ip,method,status,bytes_sent,upstream,duration_ms)
		VALUES ('request','partition-node',$1,'app.example.test','198.51.100.1','GET',200,1,'web',1)`, now)
	require.NoError(t, err)

	var interfaceTable, connectionTable, requestTable string
	require.NoError(t, pool.QueryRow(ctx, "SELECT tableoid::regclass::text FROM interface_deltas WHERE event_id='interface'").Scan(&interfaceTable))
	require.NoError(t, pool.QueryRow(ctx, "SELECT tableoid::regclass::text FROM connection_details WHERE event_id='connection'").Scan(&connectionTable))
	require.NoError(t, pool.QueryRow(ctx, "SELECT tableoid::regclass::text FROM proxy_request_details WHERE event_id='request'").Scan(&requestTable))
	suffix := now.Format("200601")
	require.Equal(t, fmt.Sprintf("interface_deltas_%s", suffix), interfaceTable)
	require.Equal(t, fmt.Sprintf("connection_details_%s", suffix), connectionTable)
	require.Equal(t, fmt.Sprintf("proxy_request_details_%s", suffix), requestTable)
}
