package interfacecounter

import (
	"os"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestStateFileRoundTripsSecureCounterCheckpoint(t *testing.T) {
	path := t.TempDir() + "/interface-counters.json"
	store := NewStateFile(path)
	at := time.Unix(2, 0).UTC()
	pending := model.Batch{SchemaVersion: 1, BatchID: "pending-batch", NodeID: "node-a", SentAt: at, Events: []model.Event{{
		ID: "event-a", ObservedAt: at, Kind: model.EventInterfaceDelta, Direction: model.DirectionInbound, Interface: "enp0s6", Bytes: 60, Packets: 1,
	}}}
	want := NewCheckpoint("node-a", map[string]Counter{"enp0s6": {RXBytes: 160, RXPackets: 2, TXBytes: 280, TXPackets: 4}}, &pending)

	require.NoError(t, store.Save(want))
	got, found, err := store.Load()

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, "-rw-------", info.Mode().String())
}

func TestStateFileReportsMissingCheckpoint(t *testing.T) {
	checkpoint, found, err := NewStateFile(t.TempDir() + "/missing.json").Load()

	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, checkpoint.Counters)
}
