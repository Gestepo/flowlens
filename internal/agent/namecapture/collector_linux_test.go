//go:build linux

package namecapture

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/bpf"
)

func TestCollectorWaitsForPacketReadersBeforeClose(t *testing.T) {
	handle := &slowTimeoutHandle{entered: make(chan struct{})}
	collector := &Collector{handles: []packetHandle{handle}, processor: NewProcessor(ProcessorOptions{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx, make(chan model.NameEvidence)) }()
	<-handle.entered
	require.NoError(t, collector.Close())
	require.NoError(t, <-done)
	require.False(t, handle.closedWhileReading.Load())
	cancel()
}

type slowTimeoutHandle struct {
	entered            chan struct{}
	reading            atomic.Bool
	closedWhileReading atomic.Bool
}

func (handle *slowTimeoutHandle) ReadPacketData() ([]byte, gopacket.CaptureInfo, error) {
	if handle.reading.CompareAndSwap(false, true) {
		close(handle.entered)
	}
	time.Sleep(50 * time.Millisecond)
	handle.reading.Store(false)
	return nil, gopacket.CaptureInfo{}, afpacket.ErrTimeout
}

func (handle *slowTimeoutHandle) Close() {
	handle.closedWhileReading.Store(handle.reading.Load())
}

func TestDecodeFrameCopiesTCPMetadataAndPayload(t *testing.T) {
	ethernet := &layers.Ethernet{SrcMAC: net.HardwareAddr{0, 1, 2, 3, 4, 5}, DstMAC: net.HardwareAddr{6, 7, 8, 9, 10, 11}, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolTCP, SrcIP: net.IPv4(10, 0, 0, 2), DstIP: net.IPv4(203, 0, 113, 10)}
	tcp := &layers.TCP{SrcPort: 42000, DstPort: 443, Seq: 1, SYN: true}
	require.NoError(t, tcp.SetNetworkLayerForChecksum(ip))
	buffer := gopacket.NewSerializeBuffer()
	require.NoError(t, gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, ethernet, ip, tcp, gopacket.Payload([]byte{1, 2, 3})))
	now := time.Now().UTC()

	packet, ok := decodeFrame(buffer.Bytes(), now)
	require.True(t, ok)
	require.Equal(t, "10.0.0.2", packet.SourceIP.String())
	require.Equal(t, "203.0.113.10", packet.DestinationIP.String())
	require.Equal(t, uint16(42000), packet.SourcePort)
	require.Equal(t, uint16(443), packet.DestinationPort)
	require.Equal(t, uint8(6), packet.Protocol)
	require.Equal(t, []byte{1, 2, 3}, packet.Payload)
}

func TestCaptureFilterAcceptsDNSAndTLSButRejectsOtherTraffic(t *testing.T) {
	filter, err := captureFilter()
	require.NoError(t, err)
	vm, err := bpf.NewVM(filter)
	require.NoError(t, err)
	require.NotZero(t, runFilter(t, vm, 443))
	require.NotZero(t, runFilter(t, vm, 53))
	require.Zero(t, runFilter(t, vm, 80))
}

func runFilter(t *testing.T, vm *bpf.VM, destinationPort layers.TCPPort) int {
	t.Helper()
	ethernet := &layers.Ethernet{SrcMAC: net.HardwareAddr{0, 1, 2, 3, 4, 5}, DstMAC: net.HardwareAddr{6, 7, 8, 9, 10, 11}, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolTCP, SrcIP: net.IPv4(10, 0, 0, 2), DstIP: net.IPv4(203, 0, 113, 10)}
	tcp := &layers.TCP{SrcPort: 42000, DstPort: destinationPort, SYN: true}
	require.NoError(t, tcp.SetNetworkLayerForChecksum(ip))
	buffer := gopacket.NewSerializeBuffer()
	require.NoError(t, gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, ethernet, ip, tcp))
	result, err := vm.Run(buffer.Bytes())
	require.NoError(t, err)
	return result
}
