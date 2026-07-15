package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompatibilityAcceptsFoundationInterfaceBatch(t *testing.T) {
	require.NoError(t, validInterfaceBatch().Validate())
}

func TestTrafficPayloadsValidate(t *testing.T) {
	for _, kind := range []EventKind{EventConnection, EventProxyRequest, EventNameEvidence, EventOwnerInventory} {
		t.Run(string(kind), func(t *testing.T) {
			require.NoError(t, validTrafficBatch(kind).Validate())
		})
	}
}

func TestTrafficValidationRejectsInvalidConnectionFields(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ConnectionDelta)
		want string
	}{
		{name: "local IP", edit: func(c *ConnectionDelta) { c.Local.IP = "bad" }, want: "local ip"},
		{name: "remote IP", edit: func(c *ConnectionDelta) { c.Remote.IP = "" }, want: "remote ip"},
		{name: "local port", edit: func(c *ConnectionDelta) { c.Local.Port = 0 }, want: "local port"},
		{name: "confidence", edit: func(c *ConnectionDelta) { c.Remote.Confidence = "guessed" }, want: "confidence"},
		{name: "sent bytes", edit: func(c *ConnectionDelta) { c.BytesSent = -1 }, want: "bytes_sent"},
		{name: "received bytes", edit: func(c *ConnectionDelta) { c.BytesReceived = -1 }, want: "bytes_received"},
		{name: "process PID", edit: func(c *ConnectionDelta) { c.Owner.PID = 0 }, want: "pid"},
		{name: "container ID", edit: func(c *ConnectionDelta) { c.Owner = OwnerRef{Kind: OwnerContainer} }, want: "container_id"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := validTrafficBatch(EventConnection)
			test.edit(batch.Events[0].Connection)
			require.ErrorContains(t, batch.Validate(), test.want)
		})
	}
}

func TestTrafficValidationRequiresExactlyOneMatchingPayload(t *testing.T) {
	batch := validTrafficBatch(EventConnection)
	batch.Events[0].NameEvidence = &NameEvidence{IP: "203.0.113.10", Name: "example.test", Source: "dns", ValidFrom: batch.SentAt, ValidUntil: batch.SentAt.Add(time.Minute)}
	require.ErrorContains(t, batch.Validate(), "exactly one payload")

	batch = validTrafficBatch(EventProxyRequest)
	batch.Events[0].ProxyRequest = nil
	require.ErrorContains(t, batch.Validate(), "exactly one payload")

	batch = validInterfaceBatch()
	batch.Events[0].Connection = validTrafficBatch(EventConnection).Events[0].Connection
	require.ErrorContains(t, batch.Validate(), "exactly one payload")
}

func validTrafficBatch(kind EventKind) Batch {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	event := Event{ID: "traffic-event", ObservedAt: now, Kind: kind}
	switch kind {
	case EventConnection:
		event.Connection = &ConnectionDelta{
			Protocol:      "tcp",
			Local:         Endpoint{IP: "10.0.0.2", Port: 42000, Confidence: ConfidenceIPOnly},
			Remote:        Endpoint{IP: "203.0.113.10", Port: 443, Domain: "example.test", Confidence: ConfidenceConfirmed},
			Owner:         OwnerRef{Kind: OwnerProcess, PID: 42, Process: "curl"},
			BytesSent:     100,
			BytesReceived: 200,
			State:         "established",
		}
	case EventProxyRequest:
		event.ProxyRequest = &ProxyRequest{Host: "app.example.test", SourceIP: "198.51.100.4", Method: "GET", Status: 200, BytesSent: 512, Upstream: "172.20.0.3:8080", DurationMS: 12}
	case EventNameEvidence:
		event.NameEvidence = &NameEvidence{IP: "203.0.113.10", Name: "example.test", Source: "dns", ValidFrom: now, ValidUntil: now.Add(time.Minute)}
	case EventOwnerInventory:
		event.OwnerInventory = &OwnerInventory{Owner: OwnerRef{Kind: OwnerContainer, ContainerID: "0123456789abcdef", ContainerName: "web"}, CgroupID: 9, Addresses: []string{"172.20.0.3"}, Ports: []uint16{8080}, Running: true}
	}
	return Batch{SchemaVersion: 1, BatchID: "traffic-batch", NodeID: "flowlens-node-1", SentAt: now, Events: []Event{event}}
}
