package namecapture

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProcessorReassemblesTLSAndEmitsTenMinuteEvidence(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	processor := NewProcessor(ProcessorOptions{MaxFlows: 8})
	hello := tlsRecord(clientHello(t, "api.example.com", false))
	packet := Packet{ObservedAt: now, SourceIP: netip.MustParseAddr("10.0.0.2"), DestinationIP: netip.MustParseAddr("203.0.113.10"), SourcePort: 42000, DestinationPort: 443, Protocol: 6}
	packet.Payload = hello[:20]
	require.Empty(t, processor.Process(packet))
	packet.Payload = hello[20:]
	evidence := processor.Process(packet)
	require.Len(t, evidence, 1)
	require.Equal(t, "api.example.com", evidence[0].Name)
	require.Equal(t, "203.0.113.10", evidence[0].IP)
	require.Equal(t, "tls_sni", evidence[0].Source)
	require.Equal(t, now.Add(10*time.Minute), evidence[0].ValidUntil)
}

func TestProcessorExpiresAndBoundsPartialTLSFlows(t *testing.T) {
	processor := NewProcessor(ProcessorOptions{MaxFlows: 1})
	now := time.Now().UTC()
	partial := tlsRecord(clientHello(t, "one.example.com", false))[:10]
	first := Packet{ObservedAt: now, SourceIP: netip.MustParseAddr("10.0.0.2"), DestinationIP: netip.MustParseAddr("203.0.113.10"), SourcePort: 1000, DestinationPort: 443, Protocol: 6, Payload: partial}
	second := first
	second.SourcePort = 1001
	processor.Process(first)
	processor.Process(second)
	require.Equal(t, uint64(1), processor.Stats().DroppedFlows)

	second.ObservedAt = now.Add(6 * time.Second)
	processor.Process(second)
	require.Equal(t, uint64(1), processor.Stats().ExpiredFlows)
}

func TestCollectorHealthEventDoesNotExposePacketData(t *testing.T) {
	event := CollectorHealthEvent(time.Now().UTC(), "collector_unavailable", 3)
	require.Equal(t, "name_capture", event.Health.Collector)
	require.Equal(t, int64(3), event.Health.DroppedEvents)
	require.Empty(t, event.Health.Message)
}
