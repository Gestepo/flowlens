package store_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"flowlens/internal/model"
	"flowlens/internal/server/geoip"
	"flowlens/internal/server/store"

	"github.com/stretchr/testify/require"
)

func TestInsertBatchAttributesOutOfOrderDetailsAndAggregatesIdempotently(t *testing.T) {
	pool := openTestDatabase(t)
	now := time.Date(2026, 7, 14, 9, 0, 30, 0, time.UTC)
	connection := model.ConnectionDelta{
		Protocol:  "tcp",
		Local:     model.Endpoint{IP: "10.0.0.2", Port: 42000, Confidence: model.ConfidenceIPOnly},
		Remote:    model.Endpoint{IP: "203.0.113.10", Port: 443, Confidence: model.ConfidenceIPOnly},
		Owner:     model.OwnerRef{Kind: model.OwnerContainer, ContainerID: "container-id", ContainerName: "web"},
		BytesSent: 100, BytesReceived: 200, State: "established",
	}
	batch := model.Batch{SchemaVersion: 1, BatchID: "detail-batch", NodeID: "flowlens-node-1", SentAt: now, Events: []model.Event{
		{ID: "connection-event", ObservedAt: now, Kind: model.EventConnection, Connection: &connection},
		{ID: "proxy-event", ObservedAt: now, Kind: model.EventProxyRequest, ProxyRequest: &model.ProxyRequest{Host: "app.example.test", SourceIP: "198.51.100.4", Method: "GET", Status: 200, BytesSent: 512, Upstream: "172.18.0.3:8080", DurationMS: 12}},
		{ID: "proxy-name-event", ObservedAt: now, Kind: model.EventProxyRequest, ProxyRequest: &model.ProxyRequest{Host: "name.example.test", SourceIP: "198.51.100.5", Method: "GET", Status: 200, BytesSent: 256, Upstream: "web", DurationMS: 8}},
		{ID: "owner-event", ObservedAt: now, Kind: model.EventOwnerInventory, OwnerInventory: &model.OwnerInventory{Owner: connection.Owner, CgroupID: 77, Addresses: []string{"172.18.0.3"}, Ports: []uint16{8080}, Running: true}},
		{ID: "compose-owner-event", ObservedAt: now, Kind: model.EventOwnerInventory, OwnerInventory: &model.OwnerInventory{Owner: model.OwnerRef{Kind: model.OwnerContainer, ContainerID: "server-container-id", ContainerName: "application-server-1"}, CgroupID: 78, Ports: []uint16{8088}, Running: true}},
		{ID: "compose-proxy-event", ObservedAt: now, Kind: model.EventProxyRequest, ProxyRequest: &model.ProxyRequest{Host: "compose.example.test", SourceIP: "198.51.100.6", Method: "GET", Status: 200, BytesSent: 128, Upstream: "application-server", DurationMS: 6}},
		{ID: "scaled-owner-1", ObservedAt: now, Kind: model.EventOwnerInventory, OwnerInventory: &model.OwnerInventory{Owner: model.OwnerRef{Kind: model.OwnerContainer, ContainerID: "scaled-container-1", ContainerName: "scaled-service-1"}, CgroupID: 79, Running: true}},
		{ID: "scaled-owner-2", ObservedAt: now, Kind: model.EventOwnerInventory, OwnerInventory: &model.OwnerInventory{Owner: model.OwnerRef{Kind: model.OwnerContainer, ContainerID: "scaled-container-2", ContainerName: "scaled-service-2"}, CgroupID: 80, Running: true}},
		{ID: "scaled-proxy-event", ObservedAt: now, Kind: model.EventProxyRequest, ProxyRequest: &model.ProxyRequest{Host: "scaled.example.test", SourceIP: "198.51.100.7", Method: "GET", Status: 200, BytesSent: 64, Upstream: "scaled-service", DurationMS: 5}},
		{ID: "evidence-event", ObservedAt: now.Add(-time.Second), Kind: model.EventNameEvidence, NameEvidence: &model.NameEvidence{IP: "203.0.113.10", Name: "api.example.test", Source: "tls_sni", ValidFrom: now.Add(-time.Second), ValidUntil: now.Add(time.Minute)}},
	}}
	trafficStore := store.New(pool, store.WithGeoIP(fakeGeoIP{}))
	inserted, err := trafficStore.InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.True(t, inserted)
	inserted, err = trafficStore.InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.False(t, inserted)
	duplicateEvents := batch
	duplicateEvents.BatchID = "detail-batch-2"
	inserted, err = trafficStore.InsertBatch(context.Background(), duplicateEvents)
	require.NoError(t, err)
	require.True(t, inserted)

	var displayName, confidence, direction, ownerID string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT display_name, confidence, direction, owner_id FROM connection_details WHERE event_id='connection-event'`).Scan(&displayName, &confidence, &direction, &ownerID))
	require.Equal(t, "api.example.test", displayName)
	require.Equal(t, "confirmed", confidence)
	require.Equal(t, "outbound", direction)
	require.Equal(t, "container:container-id", ownerID)
	var country string
	var asn int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT country_code, asn FROM connection_details WHERE event_id='connection-event'`).Scan(&country, &asn))
	require.Equal(t, "US", country)
	require.Equal(t, int64(64500), asn)

	var detailCount, aggregateBytes, aggregateConnections int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM connection_details`).Scan(&detailCount))
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT sum(bytes), sum(connections) FROM owner_minute`).Scan(&aggregateBytes, &aggregateConnections))
	require.Equal(t, int64(1), detailCount)
	require.Equal(t, int64(300), aggregateBytes)
	require.Equal(t, int64(1), aggregateConnections)

	var upstreamOwner string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT upstream_owner_id FROM proxy_request_details WHERE event_id='proxy-event'`).Scan(&upstreamOwner))
	require.Equal(t, "container:container-id", upstreamOwner)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT upstream_owner_id FROM proxy_request_details WHERE event_id='proxy-name-event'`).Scan(&upstreamOwner))
	require.Equal(t, "container:container-id", upstreamOwner)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT upstream_owner_id FROM proxy_request_details WHERE event_id='compose-proxy-event'`).Scan(&upstreamOwner))
	require.Equal(t, "container:server-container-id", upstreamOwner)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT coalesce(upstream_owner_id,'') FROM proxy_request_details WHERE event_id='scaled-proxy-event'`).Scan(&upstreamOwner))
	require.Empty(t, upstreamOwner)
	var flowOwner, flowSource, flowDestination, flowDomain string
	var flowPort int
	var flowBytes, flowConnections, flowRequests int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT owner_id,source,destination,domain,remote_port,bytes,connections,requests FROM flow_minute WHERE direction='inbound' AND domain='app.example.test'`).Scan(&flowOwner, &flowSource, &flowDestination, &flowDomain, &flowPort, &flowBytes, &flowConnections, &flowRequests))
	require.Equal(t, "container:container-id", flowOwner)
	require.Equal(t, "198.51.100.4", flowSource)
	require.Equal(t, "172.18.0.3", flowDestination)
	require.Equal(t, "app.example.test", flowDomain)
	require.Equal(t, 8080, flowPort)
	require.Equal(t, int64(512), flowBytes)
	require.Zero(t, flowConnections)
	require.Equal(t, int64(1), flowRequests)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT remote_port FROM flow_minute WHERE direction='inbound' AND domain='compose.example.test'`).Scan(&flowPort))
	require.Equal(t, 8088, flowPort)
	var requests int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT sum(requests) FROM proxy_status_minute WHERE host='app.example.test'`).Scan(&requests))
	require.Equal(t, int64(1), requests)
	disabledAt := now.Add(time.Second)
	disabled := model.Batch{SchemaVersion: 1, BatchID: "docker-disabled-batch", NodeID: "flowlens-node-1", SentAt: disabledAt, Events: []model.Event{{
		ID: "docker-disabled-event", ObservedAt: disabledAt, Kind: model.EventHealth,
		Health: &model.HealthEvent{Collector: "docker", Status: "healthy", Code: "collector_disabled", Message: "Docker attribution is disabled"},
	}}}
	inserted, err = trafficStore.InsertBatch(context.Background(), disabled)
	require.NoError(t, err)
	require.True(t, inserted)
	var runningContainers int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM owners WHERE node_id='flowlens-node-1' AND kind='container' AND running=true`).Scan(&runningContainers))
	require.Zero(t, runningContainers)
}

func TestInsertBatchAttributesProxyUpstreamToObservedProcess(t *testing.T) {
	pool := openTestDatabase(t)
	now := time.Date(2026, 7, 16, 4, 0, 30, 0, time.UTC)
	serverConnection := model.ConnectionDelta{
		Protocol: "tcp",
		Local:    model.Endpoint{IP: "172.18.0.1", Port: 8088, Confidence: model.ConfidenceIPOnly},
		Remote:   model.Endpoint{IP: "172.18.0.20", Port: 44000, Confidence: model.ConfidenceIPOnly},
		Owner:    model.OwnerRef{Kind: model.OwnerProcess, PID: 4242, Process: "flowlens-server"},
		State:    "established",
	}
	batch := model.Batch{SchemaVersion: 1, BatchID: "native-proxy-batch", NodeID: "flowlens-node-1", SentAt: now, Events: []model.Event{
		{ID: "native-server-connection", ObservedAt: now, Kind: model.EventConnection, Connection: &serverConnection},
		{ID: "native-proxy-request", ObservedAt: now.Add(time.Second), Kind: model.EventProxyRequest, ProxyRequest: &model.ProxyRequest{
			Host: "monitor.example.com", SourceIP: "198.51.100.20", Method: "GET", Status: 200, BytesSent: 1024, Upstream: "172.18.0.1:8088",
		}},
	}}

	inserted, err := store.New(pool).InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.True(t, inserted)

	var upstreamOwner string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT upstream_owner_id FROM proxy_request_details WHERE event_id='native-proxy-request'`).Scan(&upstreamOwner))
	require.Equal(t, "process:4242:flowlens-server", upstreamOwner)
	var flowOwner string
	var flowPort int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT owner_id,remote_port FROM flow_minute WHERE direction='inbound' AND domain='monitor.example.com'`).Scan(&flowOwner, &flowPort))
	require.Equal(t, "process:4242:flowlens-server", flowOwner)
	require.Equal(t, 8088, flowPort)
}

type fakeGeoIP struct{}

func (fakeGeoIP) Lookup(netip.Addr) geoip.NetworkInfo {
	return geoip.NetworkInfo{CountryCode: "US", CountryName: "United States", ASN: 64500, Organization: "Example Network", Classification: "public"}
}
