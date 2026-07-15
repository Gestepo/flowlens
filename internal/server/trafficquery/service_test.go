package trafficquery

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/internal/model"
	"flowlens/migrations"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestServiceReturnsDirectionIsolatedDetailedViews(t *testing.T) {
	pool := queryTestDatabase(t)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	seedQueries(t, pool, now)
	service := NewService(pool)
	base := Filter{NodeID: "flowlens-node-1", Start: now.Add(-time.Hour), End: now.Add(time.Hour), Limit: 50, Sort: "bytes"}

	live, err := service.Live(context.Background(), base)
	require.NoError(t, err)
	require.Len(t, live.Items, 1)
	require.Equal(t, "api.example.test", live.Items[0].DisplayName)
	require.Equal(t, "10.0.0.2:42000", live.Items[0].Source)

	outbound := base
	outbound.Direction = model.DirectionOutbound
	domains, err := service.Domains(context.Background(), outbound)
	require.NoError(t, err)
	require.Len(t, domains.Items, 1)
	require.Equal(t, "api.example.test", domains.Items[0].Domain)
	require.Equal(t, int64(1), domains.Items[0].OwnerCount)
	outbound.Domain = "api.example.test"
	outbound.Confidence = model.ConfidenceConfirmed
	outboundDetail, err := service.DomainDetail(context.Background(), outbound)
	require.NoError(t, err)
	require.Equal(t, []DomainNetwork{{CountryCode: "US", CountryName: "United States", ASN: 64500, Organization: "Example Network", Classification: "public", Connections: 1, Bytes: 300}}, outboundDetail.Networks)
	inbound := base
	inbound.Direction = model.DirectionInbound
	domains, err = service.Domains(context.Background(), inbound)
	require.NoError(t, err)
	require.Len(t, domains.Items, 1)
	require.Equal(t, "app.example.test", domains.Items[0].Domain)
	require.Equal(t, int64(1), domains.Items[0].Requests)
	require.Equal(t, int64(1), domains.Items[0].OwnerCount)
	inbound.Domain = "app.example.test"
	inbound.Confidence = model.ConfidenceConfirmed
	detail, err := service.DomainDetail(context.Background(), inbound)
	require.NoError(t, err)
	require.Equal(t, "app.example.test", detail.Domain)
	require.Equal(t, []DomainStatus{{Status: 200, Requests: 3, Bytes: 400}, {Status: 502, Requests: 1, Bytes: 112}}, detail.Statuses)
	require.Equal(t, []DomainSource{{IP: "198.51.100.10", Requests: 1, Bytes: 512}}, detail.Sources)
	require.Equal(t, []DomainOwner{{ID: "container:web", Name: "web", Requests: 1, Bytes: 512}}, detail.Owners)

	owners, err := service.Owners(context.Background(), base)
	require.NoError(t, err)
	require.Len(t, owners.Items, 2)
	require.Equal(t, "web", owners.Items[0].Name)
	require.Equal(t, int64(300), owners.Items[0].Bytes)
	require.Equal(t, []uint16{8080}, owners.Items[0].Ports)
	require.Equal(t, "idle", owners.Items[1].Name)
	require.Zero(t, owners.Items[1].Bytes)
	byName := base
	byName.OwnerID = "web"
	owners, err = service.Owners(context.Background(), byName)
	require.NoError(t, err)
	require.Len(t, owners.Items, 1)
	ownerFilter := base
	ownerFilter.OwnerID = "container:web"
	ownerDetail, err := service.OwnerDetail(context.Background(), ownerFilter)
	require.NoError(t, err)
	require.Equal(t, "web", ownerDetail.Name)
	require.Len(t, ownerDetail.Series, 1)
	require.Equal(t, int64(300), ownerDetail.Series[0].OutboundBytes)
	require.Len(t, ownerDetail.ActiveConnections, 1)
	require.Equal(t, "api.example.test", ownerDetail.ActiveConnections[0].DisplayName)

	flows, err := service.Flows(context.Background(), outbound)
	require.NoError(t, err)
	require.Len(t, flows.Items, 1)
	require.Equal(t, "US", flows.Items[0].CountryCode)
	require.Equal(t, uint32(64500), flows.Items[0].ASN)
	require.Equal(t, int64(1), flows.Items[0].Connections)
	require.Zero(t, flows.Items[0].Requests)
	require.NotEmpty(t, flows.DataFreshAt)
	inboundFlows, err := service.Flows(context.Background(), inbound)
	require.NoError(t, err)
	require.Len(t, inboundFlows.Items, 1)
	require.Equal(t, "app.example.test", inboundFlows.Items[0].Domain)
	require.Equal(t, "198.51.100.10", inboundFlows.Items[0].Source)
	require.Equal(t, "container:web", inboundFlows.Items[0].OwnerID)
	require.Zero(t, inboundFlows.Items[0].Connections)
	require.Equal(t, int64(1), inboundFlows.Items[0].Requests)
}

func TestInboundDomainKeepsLiteralUpstreamWhenHistoricalOwnerIDIsMissing(t *testing.T) {
	pool := queryTestDatabase(t)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	seedQueries(t, pool, now)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO proxy_request_details(event_id,node_id,observed_at,host,source_ip,method,status,bytes_sent,upstream,upstream_owner_id,duration_ms)
		VALUES ('legacy-proxy','flowlens-node-1',$1,'app.example.test','198.51.100.11','GET',200,256,'legacy-web','',5)
	`, now)
	require.NoError(t, err)

	filter := Filter{NodeID: "flowlens-node-1", Start: now.Add(-time.Hour), End: now.Add(time.Hour),
		Direction: model.DirectionInbound, Domain: "app.example.test", Confidence: model.ConfidenceConfirmed, Limit: 50, Sort: "bytes"}
	service := NewService(pool)
	domains, err := service.Domains(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, domains.Items, 1)
	require.Equal(t, int64(2), domains.Items[0].OwnerCount)
	detail, err := service.DomainDetail(context.Background(), filter)
	require.NoError(t, err)
	require.Contains(t, detail.Owners, DomainOwner{ID: "upstream:legacy-web", Name: "legacy-web", Requests: 1, Bytes: 256})
}

func TestLiveReturnsCurrentPeakRatesAndActiveConnections(t *testing.T) {
	pool := queryTestDatabase(t)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO nodes(id,name,last_seen_at) VALUES ('flowlens-node-1','flowlens-node-1',$1);
		INSERT INTO interface_deltas(event_id,node_id,observed_at,interface,direction,bytes,packets) VALUES
		('in-current','flowlens-node-1',$1::timestamptz - interval '5 seconds','eth0','inbound',1000,1),
		('out-current','flowlens-node-1',$1::timestamptz - interval '5 seconds','eth0','outbound',2000,1);
		INSERT INTO traffic_minute(node_id,bucket,direction,bytes,packets) VALUES
		('flowlens-node-1',$1::timestamptz - interval '2 minutes','inbound',6000,1),
		('flowlens-node-1',$1::timestamptz - interval '2 minutes','outbound',12000,1);
		INSERT INTO connection_details(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,bytes_sent,bytes_received,state) VALUES
		('active','flowlens-node-1',$1::timestamptz - interval '5 seconds','outbound','tcp','10.0.0.2',42000,'203.0.113.10',443,'host','host','Host','203.0.113.10','ip_only',10,20,'established');
	`, pgx.QueryExecModeSimpleProtocol, now)
	require.NoError(t, err)
	service := NewService(pool)
	result, err := service.Live(context.Background(), Filter{NodeID: "flowlens-node-1", Start: now.Add(-time.Hour), End: now, Limit: 50, Sort: "time"})
	require.NoError(t, err)
	require.NotNil(t, result.Metrics)
	require.Equal(t, LiveMetrics{CurrentInboundBPS: 100, CurrentOutboundBPS: 200, PeakInboundBPS: 100, PeakOutboundBPS: 200, ActiveConnections: 1}, *result.Metrics)
}

func TestTrafficResponseDistinguishesRecoveredAndCurrentCollectorGaps(t *testing.T) {
	pool := queryTestDatabase(t)
	now := time.Date(2026, 7, 15, 6, 10, 0, 0, time.UTC)
	seedQueries(t, pool, now)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO collector_health(event_id,node_id,observed_at,collector,status,code) VALUES
		('npm-degraded','flowlens-node-1',$1::timestamptz - interval '10 minutes','npm_logs','degraded','malformed_lines'),
		('npm-healthy','flowlens-node-1',$1::timestamptz - interval '5 minutes','npm_logs','healthy','active'),
		('conntrack-degraded','flowlens-node-1',$1::timestamptz - interval '3 minutes','conntrack','degraded','collector_unavailable')
	`, now)
	require.NoError(t, err)

	result, err := NewService(pool).Domains(context.Background(), Filter{
		NodeID: "flowlens-node-1", Start: now.Add(-time.Hour), End: now.Add(time.Hour), Limit: 50, Sort: "bytes",
	})
	require.NoError(t, err)
	require.Len(t, result.PartialData, 2)
	gaps := map[string]CollectorGap{}
	for _, gap := range result.PartialData {
		gaps[gap.Collector] = gap
	}
	require.Equal(t, "malformed_lines", gaps["npm_logs"].Code)
	require.True(t, gaps["npm_logs"].At.Equal(now.Add(-10*time.Minute)))
	require.True(t, gaps["npm_logs"].Recovered)
	require.Equal(t, "collector_unavailable", gaps["conntrack"].Code)
	require.True(t, gaps["conntrack"].At.Equal(now.Add(-3*time.Minute)))
	require.False(t, gaps["conntrack"].Recovered)
}

func queryTestDatabase(t *testing.T) *pgxpool.Pool {
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

func seedQueries(t *testing.T, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO nodes(id,name,last_seen_at) VALUES ('flowlens-node-1','flowlens-node-1',$1);
		INSERT INTO owners(node_id,owner_id,kind,display_name,container_id,ports,running,first_seen_at,last_seen_at)
		VALUES ('flowlens-node-1','container:web','container','web','web','[8080]',true,$1,$1),
		('flowlens-node-1','container:idle','container','idle','idle','[]',true,$1,$1);
		INSERT INTO connection_details(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,bytes_sent,bytes_received,state,country_code,country_name,asn,organization,network_classification)
		VALUES ('c1','flowlens-node-1',$1,'outbound','tcp','10.0.0.2',42000,'203.0.113.10',443,'container:web','container','web','api.example.test','confirmed',100,200,'established','US','United States',64500,'Example Network','public');
		INSERT INTO owner_minute(node_id,bucket,owner_id,owner_kind,owner_name,direction,bytes,connections)
		VALUES ('flowlens-node-1',date_trunc('minute',$1::timestamptz),'container:web','container','web','outbound',300,1);
		INSERT INTO domain_minute(node_id,bucket,direction,domain,confidence,bytes,connections,requests) VALUES
		('flowlens-node-1',date_trunc('minute',$1::timestamptz),'outbound','api.example.test','confirmed',300,1,0),
		('flowlens-node-1',date_trunc('minute',$1::timestamptz),'inbound','app.example.test','confirmed',512,0,1);
		INSERT INTO proxy_request_details(event_id,node_id,observed_at,host,source_ip,method,status,bytes_sent,upstream,upstream_owner_id,duration_ms)
		VALUES ('p1','flowlens-node-1',$1,'app.example.test','198.51.100.10','GET',200,512,'10.0.0.2:8080','container:web',10);
		INSERT INTO proxy_status_minute(node_id,bucket,host,status,bytes,requests) VALUES
		('flowlens-node-1',date_trunc('minute',$1::timestamptz),'app.example.test',200,400,3),
		('flowlens-node-1',date_trunc('minute',$1::timestamptz),'app.example.test',502,112,1);
		INSERT INTO flow_minute(node_id,bucket,direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,country_code,country_name,asn,organization,network_classification,bytes,connections,requests)
		VALUES ('flowlens-node-1',date_trunc('minute',$1::timestamptz),'outbound','container:web','web','10.0.0.2','203.0.113.10','api.example.test','confirmed','tcp',443,'US','United States',64500,'Example Network','public',300,1,0),
		('flowlens-node-1',date_trunc('minute',$1::timestamptz),'inbound','container:web','web','198.51.100.10','10.0.0.2','app.example.test','confirmed','tcp',8080,'US','United States',64501,'Visitor Network','public',512,0,1);
	`, pgx.QueryExecModeSimpleProtocol, now)
	require.NoError(t, err)
}
