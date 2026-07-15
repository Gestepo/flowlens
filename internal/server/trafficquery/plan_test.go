package trafficquery

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestMillionRowDetailPlanPrunesUnrequestedMonths(t *testing.T) {
	rowCount, err := strconv.Atoi(os.Getenv("FLOWLENS_QUERY_PLAN_ROWS"))
	if err != nil || rowCount == 0 {
		t.Skip("FLOWLENS_QUERY_PLAN_ROWS is not configured")
	}
	require.GreaterOrEqual(t, rowCount, 1_000_000)
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	require.NotEmpty(t, url)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, migrations.Apply(ctx, pool))
	_, err = pool.Exec(ctx, "TRUNCATE connection_details, nodes CASCADE")
	require.NoError(t, err)
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, "INSERT INTO nodes(id,name,last_seen_at) VALUES ('plan-node','plan-node',$1)", now)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO connection_details(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,bytes_sent,bytes_received,state)
		SELECT 'plan-'||sample,'plan-node',$2::timestamptz-(sample % 7776000)*interval '1 second','outbound','tcp','10.0.0.2',40000,'1.1.1.1',443,'host','host','Host','1.1.1.1','ip_only',sample % 10000,100,'established'
		FROM generate_series(1,$1) sample
	`, rowCount, now)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "ANALYZE connection_details")
	require.NoError(t, err)

	var raw []byte
	err = pool.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON)
		SELECT event_id,observed_at FROM connection_details
		WHERE node_id='plan-node' AND observed_at >= $1 AND observed_at < $2
		ORDER BY bytes_sent+bytes_received DESC,observed_at DESC,event_id DESC LIMIT 50
	`, now.Add(-24*time.Hour), now.Add(time.Second)).Scan(&raw)
	require.NoError(t, err)
	var plans []explainRoot
	require.NoError(t, json.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)
	require.Less(t, plans[0].ExecutionTime, 2000.0)
	var relations []string
	collectRelations(plans[0].Plan, &relations)
	require.NotEmpty(t, relations)
	require.NotContains(t, relations, "connection_details_default")
	require.Subset(t, []string{"connection_details_" + now.Format("200601")}, relations)
	t.Logf("query-plan evidence: rows=%d execution_ms=%.3f relations=%v", rowCount, plans[0].ExecutionTime, relations)
}

type explainRoot struct {
	Plan          explainNode `json:"Plan"`
	ExecutionTime float64     `json:"Execution Time"`
}

type explainNode struct {
	Relation string        `json:"Relation Name"`
	Plans    []explainNode `json:"Plans"`
}

func collectRelations(node explainNode, output *[]string) {
	if node.Relation != "" {
		*output = append(*output, node.Relation)
	}
	for _, child := range node.Plans {
		collectRelations(child, output)
	}
}
