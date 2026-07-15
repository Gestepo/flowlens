package namecapture

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"flowlens/internal/model"
)

type Packet struct {
	ObservedAt      time.Time
	SourceIP        netip.Addr
	DestinationIP   netip.Addr
	SourcePort      uint16
	DestinationPort uint16
	Protocol        uint8
	Payload         []byte
}

type ProcessorOptions struct {
	MaxFlows int
}

type ProcessorStats struct {
	DroppedFlows uint64
	ExpiredFlows uint64
	Malformed    uint64
}

type flowKey struct {
	SourceIP        netip.Addr
	DestinationIP   netip.Addr
	SourcePort      uint16
	DestinationPort uint16
}

type partialFlow struct {
	updatedAt time.Time
	payload   []byte
}

type Processor struct {
	maxFlows int
	flows    map[flowKey]partialFlow
	stats    ProcessorStats
}

func EvidenceEvent(evidence model.NameEvidence) model.Event {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s", evidence.IP, evidence.Name, evidence.Source, evidence.ValidFrom.Format(time.RFC3339Nano))))
	evidenceCopy := evidence
	return model.Event{
		ID:           fmt.Sprintf("evidence:%x", digest[:12]),
		ObservedAt:   evidence.ValidFrom.UTC(),
		Kind:         model.EventNameEvidence,
		NameEvidence: &evidenceCopy,
	}
}

func CollectorHealthEvent(observedAt time.Time, code string, dropped uint64) model.Event {
	digest := sha256.Sum256([]byte(fmt.Sprintf("name-health:%s:%d", code, observedAt.UnixNano())))
	return model.Event{
		ID:         fmt.Sprintf("health:%x", digest[:12]),
		ObservedAt: observedAt.UTC(),
		Kind:       model.EventHealth,
		Health: &model.HealthEvent{
			Collector:     "name_capture",
			Status:        "degraded",
			Code:          code,
			DroppedEvents: clampDropped(dropped),
		},
	}
}

func clampDropped(value uint64) int64 {
	if value > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1)
	}
	return int64(value)
}

func NewProcessor(options ProcessorOptions) *Processor {
	if options.MaxFlows <= 0 {
		options.MaxFlows = 4096
	}
	return &Processor{maxFlows: options.MaxFlows, flows: make(map[flowKey]partialFlow)}
}

func (processor *Processor) Process(packet Packet) []model.NameEvidence {
	processor.expire(packet.ObservedAt)
	if packet.Protocol == 17 && (packet.SourcePort == 53 || packet.DestinationPort == 53) {
		evidence, err := ParseDNSMessage(packet.Payload, packet.ObservedAt)
		if err != nil {
			processor.stats.Malformed++
			return nil
		}
		return evidence
	}
	if packet.Protocol != 6 || packet.DestinationPort != 443 || len(packet.Payload) == 0 {
		return nil
	}
	key := flowKey{SourceIP: packet.SourceIP, DestinationIP: packet.DestinationIP, SourcePort: packet.SourcePort, DestinationPort: packet.DestinationPort}
	flow, found := processor.flows[key]
	if !found && len(processor.flows) >= processor.maxFlows {
		processor.stats.DroppedFlows++
		return nil
	}
	if len(flow.payload)+len(packet.Payload) > maxClientHelloBytes {
		delete(processor.flows, key)
		processor.stats.DroppedFlows++
		return nil
	}
	flow.payload = append(flow.payload, packet.Payload...)
	flow.updatedAt = packet.ObservedAt
	processor.flows[key] = flow
	name, ok, err := ExtractSNI(flow.payload)
	if err != nil {
		if strings.Contains(err.Error(), "truncated") || strings.Contains(err.Error(), "missing TLS handshake header") {
			return nil
		}
		delete(processor.flows, key)
		processor.stats.Malformed++
		return nil
	}
	if !ok {
		delete(processor.flows, key)
		return nil
	}
	delete(processor.flows, key)
	return []model.NameEvidence{{
		IP:         packet.DestinationIP.String(),
		Name:       name,
		Source:     "tls_sni",
		ValidFrom:  packet.ObservedAt.UTC(),
		ValidUntil: packet.ObservedAt.UTC().Add(10 * time.Minute),
	}}
}

func (processor *Processor) Stats() ProcessorStats { return processor.stats }

func (processor *Processor) expire(now time.Time) {
	for key, flow := range processor.flows {
		if now.Sub(flow.updatedAt) > 5*time.Second {
			delete(processor.flows, key)
			processor.stats.ExpiredFlows++
		}
	}
}
