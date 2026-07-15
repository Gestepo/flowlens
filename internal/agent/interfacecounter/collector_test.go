package interfacecounter

import (
	"strings"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestParseAndDeltaPhysicalInterfaceCounters(t *testing.T) {
	previous, err := Parse(strings.NewReader(procNetDev(
		"enp0s6: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0",
		"lo: 100 1 0 0 0 0 0 0 100 1 0 0 0 0 0 0",
		"docker0: 200 2 0 0 0 0 0 0 300 3 0 0 0 0 0 0",
	)))
	require.NoError(t, err)
	current, err := Parse(strings.NewReader(procNetDev(
		"enp0s6: 1600 16 0 0 0 0 0 0 2500 25 0 0 0 0 0 0",
		"lo: 300 3 0 0 0 0 0 0 300 3 0 0 0 0 0 0",
		"docker0: 500 5 0 0 0 0 0 0 700 7 0 0 0 0 0 0",
	)))
	require.NoError(t, err)
	at := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)

	events := Delta(previous, current, DefaultAllowed, at)

	require.Equal(t, []model.Event{
		{ID: "interface:enp0s6:inbound:1784019600000000000", ObservedAt: at, Kind: model.EventInterfaceDelta, Direction: model.DirectionInbound, Bytes: 600, Packets: 6, Interface: "enp0s6"},
		{ID: "interface:enp0s6:outbound:1784019600000000000", ObservedAt: at, Kind: model.EventInterfaceDelta, Direction: model.DirectionOutbound, Bytes: 500, Packets: 5, Interface: "enp0s6"},
	}, events)
}

func TestDeltaSkipsDirectionAfterCounterReset(t *testing.T) {
	previous := map[string]Counter{"enp0s6": {RXBytes: 100, RXPackets: 10, TXBytes: 200, TXPackets: 20}}
	current := map[string]Counter{"enp0s6": {RXBytes: 10, RXPackets: 1, TXBytes: 250, TXPackets: 25}}

	events := Delta(previous, current, DefaultAllowed, time.Unix(1, 0).UTC())

	require.Len(t, events, 1)
	require.Equal(t, model.DirectionOutbound, events[0].Direction)
	require.Equal(t, int64(50), events[0].Bytes)
}

func TestParseRejectsMalformedCounterLine(t *testing.T) {
	_, err := Parse(strings.NewReader(procNetDev("enp0s6: 100 invalid")))

	require.ErrorContains(t, err, "enp0s6")
}

func procNetDev(lines ...string) string {
	return "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n" + strings.Join(lines, "\n") + "\n"
}
