//go:build linux

package namecapture

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"flowlens/internal/model"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/bpf"
)

type Collector struct {
	handles     []packetHandle
	processor   *Processor
	closeOnce   sync.Once
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	runDone     chan struct{}
}

type packetHandle interface {
	ReadPacketData() ([]byte, gopacket.CaptureInfo, error)
	Close()
}

func NewCollector(interfaces []string) (*Collector, error) {
	collector := &Collector{processor: NewProcessor(ProcessorOptions{})}
	filter, err := captureFilter()
	if err != nil {
		return nil, err
	}
	rawFilter, err := bpf.Assemble(filter)
	if err != nil {
		return nil, fmt.Errorf("assemble name capture BPF filter: %w", err)
	}
	for _, name := range interfaces {
		handle, err := afpacket.NewTPacket(afpacket.OptInterface(name), afpacket.OptPollTimeout(500*time.Millisecond))
		if err != nil {
			collector.Close()
			return nil, fmt.Errorf("open AF_PACKET interface %s: %w", name, err)
		}
		if err := handle.SetBPF(rawFilter); err != nil {
			handle.Close()
			collector.Close()
			return nil, fmt.Errorf("attach name capture filter to %s: %w", name, err)
		}
		collector.handles = append(collector.handles, handle)
	}
	if len(collector.handles) == 0 {
		return nil, fmt.Errorf("name capture requires at least one interface")
	}
	return collector, nil
}

func captureFilter() ([]bpf.Instruction, error) {
	instructions := []bpf.Instruction{
		bpf.LoadAbsolute{Off: 12, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 10, SkipFalse: 20},
		bpf.LoadAbsolute{Off: 23, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipFalse: 17},
		bpf.LoadMemShift{Off: 14},
		bpf.LoadIndirect{Off: 14, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 53, SkipTrue: 13},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 443, SkipTrue: 12},
		bpf.LoadIndirect{Off: 16, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 53, SkipTrue: 10},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 443, SkipTrue: 9, SkipFalse: 10},
		bpf.LoadAbsolute{Off: 20, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipFalse: 7},
		bpf.LoadAbsolute{Off: 54, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 53, SkipTrue: 4},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 443, SkipTrue: 3},
		bpf.LoadAbsolute{Off: 56, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 53, SkipTrue: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 443, SkipFalse: 1},
		bpf.RetConstant{Val: 65535},
		bpf.RetConstant{Val: 0},
	}
	if _, err := bpf.Assemble(instructions); err != nil {
		return nil, fmt.Errorf("validate name capture BPF filter: %w", err)
	}
	return instructions, nil
}

func (collector *Collector) Run(ctx context.Context, output chan<- model.NameEvidence) error {
	runCtx, cancel := context.WithCancel(ctx)
	runDone := make(chan struct{})
	collector.lifecycleMu.Lock()
	collector.cancel = cancel
	collector.runDone = runDone
	collector.lifecycleMu.Unlock()
	var readers sync.WaitGroup
	defer func() {
		cancel()
		readers.Wait()
		close(runDone)
	}()
	packets := make(chan Packet, 1024)
	errorsChannel := make(chan error, len(collector.handles))
	for _, handle := range collector.handles {
		handle := handle
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				data, captureInfo, err := handle.ReadPacketData()
				if errors.Is(err, afpacket.ErrTimeout) {
					if runCtx.Err() != nil {
						return
					}
					continue
				}
				if err != nil {
					if runCtx.Err() == nil {
						errorsChannel <- err
					}
					return
				}
				packet, ok := decodeFrame(data, captureInfo.Timestamp.UTC())
				if !ok {
					continue
				}
				select {
				case packets <- packet:
				case <-runCtx.Done():
					return
				}
			}
		}()
	}
	for {
		select {
		case packet := <-packets:
			for _, evidence := range collector.processor.Process(packet) {
				select {
				case output <- evidence:
				case <-runCtx.Done():
					return nil
				}
			}
		case err := <-errorsChannel:
			return fmt.Errorf("read AF_PACKET name evidence: %w", err)
		case <-runCtx.Done():
			return nil
		}
	}
}

func (collector *Collector) Close() error {
	collector.closeOnce.Do(func() {
		collector.lifecycleMu.Lock()
		cancel := collector.cancel
		runDone := collector.runDone
		collector.lifecycleMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if runDone != nil {
			<-runDone
		}
		for _, handle := range collector.handles {
			handle.Close()
		}
	})
	return nil
}

func (collector *Collector) Stats() ProcessorStats { return collector.processor.Stats() }

func decodeFrame(data []byte, observedAt time.Time) (Packet, bool) {
	decoded := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
	packet := Packet{ObservedAt: observedAt}
	if ipv4Layer := decoded.Layer(layers.LayerTypeIPv4); ipv4Layer != nil {
		ipv4 := ipv4Layer.(*layers.IPv4)
		packet.SourceIP, _ = netip.AddrFromSlice(ipv4.SrcIP)
		packet.DestinationIP, _ = netip.AddrFromSlice(ipv4.DstIP)
	} else if ipv6Layer := decoded.Layer(layers.LayerTypeIPv6); ipv6Layer != nil {
		ipv6 := ipv6Layer.(*layers.IPv6)
		packet.SourceIP, _ = netip.AddrFromSlice(ipv6.SrcIP)
		packet.DestinationIP, _ = netip.AddrFromSlice(ipv6.DstIP)
	} else {
		return Packet{}, false
	}
	if tcpLayer := decoded.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp := tcpLayer.(*layers.TCP)
		packet.Protocol = 6
		packet.SourcePort = uint16(tcp.SrcPort)
		packet.DestinationPort = uint16(tcp.DstPort)
		packet.Payload = append([]byte(nil), tcp.LayerPayload()...)
		return packet, true
	}
	if udpLayer := decoded.Layer(layers.LayerTypeUDP); udpLayer != nil {
		udp := udpLayer.(*layers.UDP)
		packet.Protocol = 17
		packet.SourcePort = uint16(udp.SrcPort)
		packet.DestinationPort = uint16(udp.DstPort)
		packet.Payload = append([]byte(nil), udp.LayerPayload()...)
		return packet, true
	}
	return Packet{}, false
}
