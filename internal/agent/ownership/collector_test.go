package ownership

import (
	"net/netip"
	"testing"
	"time"

	dockerinventory "flowlens/internal/agent/docker"
	flowebpf "flowlens/internal/agent/ebpf"
	processlookup "flowlens/internal/agent/process"
	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestAttributorPrefersContainerThenProcessThenHost(t *testing.T) {
	lookup := fakeProcessLookup{values: map[uint32]processlookup.Process{42: {PID: 42, Name: "curl"}}}
	attributor := NewAttributor(lookup)
	observation := testObservation()

	connection := attributor.Attribute(observation)
	require.Equal(t, model.OwnerProcess, connection.Owner.Kind)
	require.Equal(t, "curl", connection.Owner.Process)

	attributor.SetSnapshot(dockerinventory.Snapshot{ByCgroup: map[uint64]dockerinventory.Container{
		77: {ID: "container-id", Name: "web", CgroupID: 77, Running: true},
	}})
	connection = attributor.Attribute(observation)
	require.Equal(t, model.OwnerContainer, connection.Owner.Kind)
	require.Equal(t, "web", connection.Owner.ContainerName)

	observation.CgroupID = 88
	observation.PID = 999
	connection = attributor.Attribute(observation)
	require.Equal(t, model.OwnerHost, connection.Owner.Kind)
}

func TestCollectorAggregatesMatchingConnectionDeltas(t *testing.T) {
	attributor := NewAttributor(fakeProcessLookup{})
	collector := NewCollector(attributor)
	observation := testObservation()
	collector.Observe(observation)
	observation.Sent = 5
	observation.Received = 7
	collector.Observe(observation)

	events := collector.Drain(time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	require.Len(t, events, 1)
	require.Equal(t, model.EventConnection, events[0].Kind)
	require.Equal(t, int64(105), events[0].Connection.BytesSent)
	require.Equal(t, int64(207), events[0].Connection.BytesReceived)
	require.Empty(t, collector.Drain(time.Now()))
}

func TestCollectorDropsIncompleteKernelEndpoints(t *testing.T) {
	collector := NewCollector(NewAttributor(fakeProcessLookup{}))
	observation := testObservation()
	observation.RemotePort = 0
	collector.Observe(observation)
	observation = testObservation()
	observation.LocalPort = 0
	collector.Observe(observation)

	require.Empty(t, collector.Drain(time.Now()))
}

func TestInventoryEventsIncludeContainersWithoutTraffic(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	events := InventoryEvents(dockerinventory.Snapshot{ByID: map[string]dockerinventory.Container{
		"container-id": {ID: "container-id", Name: "idle-worker", CgroupID: 77, Addresses: []string{"172.18.0.3"}, Ports: []uint16{9000}, Running: true},
	}}, now)
	require.Len(t, events, 1)
	require.Equal(t, model.EventOwnerInventory, events[0].Kind)
	require.Equal(t, "idle-worker", events[0].OwnerInventory.Owner.ContainerName)
	require.True(t, events[0].OwnerInventory.Running)
}

func testObservation() flowebpf.Observation {
	return flowebpf.Observation{
		MonotonicNS: 1,
		CgroupID:    77,
		PID:         42,
		Protocol:    6,
		LocalIP:     mustAddr("10.0.0.2"),
		RemoteIP:    mustAddr("203.0.113.10"),
		LocalPort:   42000,
		RemotePort:  443,
		Sent:        100,
		Received:    200,
		State:       1,
	}
}

func mustAddr(value string) netip.Addr { return netip.MustParseAddr(value) }

type fakeProcessLookup struct {
	values map[uint32]processlookup.Process
}

func (lookup fakeProcessLookup) Lookup(pid uint32) (processlookup.Process, bool) {
	value, ok := lookup.values[pid]
	return value, ok
}
