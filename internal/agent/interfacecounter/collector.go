package interfacecounter

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"flowlens/internal/model"
)

type Counter struct {
	RXBytes   uint64
	RXPackets uint64
	TXBytes   uint64
	TXPackets uint64
}

func Parse(reader io.Reader) (map[string]Counter, error) {
	counters := make(map[string]Counter)
	scanner := bufio.NewScanner(reader)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		name, values, ok := strings.Cut(line, ":")
		if !ok {
			if lineNumber <= 2 {
				continue
			}
			return nil, fmt.Errorf("parse /proc/net/dev line %d: missing interface separator", lineNumber)
		}
		name = strings.TrimSpace(name)
		fields := strings.Fields(values)
		if name == "" || len(fields) < 16 {
			return nil, fmt.Errorf("parse interface %q: expected 16 counters", name)
		}
		rxBytes, err := parseCounter(name, "rx_bytes", fields[0])
		if err != nil {
			return nil, err
		}
		rxPackets, err := parseCounter(name, "rx_packets", fields[1])
		if err != nil {
			return nil, err
		}
		txBytes, err := parseCounter(name, "tx_bytes", fields[8])
		if err != nil {
			return nil, err
		}
		txPackets, err := parseCounter(name, "tx_packets", fields[9])
		if err != nil {
			return nil, err
		}
		counters[name] = Counter{RXBytes: rxBytes, RXPackets: rxPackets, TXBytes: txBytes, TXPackets: txPackets}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read /proc/net/dev: %w", err)
	}
	return counters, nil
}

func Delta(previous, current map[string]Counter, allowed func(string) bool, observedAt time.Time) []model.Event {
	names := make([]string, 0, len(current))
	for name := range current {
		if _, ok := previous[name]; ok && allowed(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	events := make([]model.Event, 0, len(names)*2)
	for _, name := range names {
		before := previous[name]
		after := current[name]
		if event, ok := directionDelta(name, model.DirectionInbound, before.RXBytes, after.RXBytes, before.RXPackets, after.RXPackets, observedAt); ok {
			events = append(events, event)
		}
		if event, ok := directionDelta(name, model.DirectionOutbound, before.TXBytes, after.TXBytes, before.TXPackets, after.TXPackets, observedAt); ok {
			events = append(events, event)
		}
	}
	return events
}

func DefaultAllowed(name string) bool {
	return name != "lo" && name != "docker0" && !strings.HasPrefix(name, "br-") && !strings.HasPrefix(name, "veth")
}

func parseCounter(iface, field, value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse interface %q %s: %w", iface, field, err)
	}
	return parsed, nil
}

func directionDelta(iface string, direction model.Direction, beforeBytes, afterBytes, beforePackets, afterPackets uint64, observedAt time.Time) (model.Event, bool) {
	if afterBytes < beforeBytes || afterPackets < beforePackets {
		return model.Event{}, false
	}
	bytesDelta := afterBytes - beforeBytes
	packetsDelta := afterPackets - beforePackets
	if (bytesDelta == 0 && packetsDelta == 0) || bytesDelta > math.MaxInt64 || packetsDelta > math.MaxInt64 {
		return model.Event{}, false
	}
	return model.Event{
		ID:         fmt.Sprintf("interface:%s:%s:%d", iface, direction, observedAt.UnixNano()),
		ObservedAt: observedAt,
		Kind:       model.EventInterfaceDelta,
		Direction:  direction,
		Bytes:      int64(bytesDelta),
		Packets:    int64(packetsDelta),
		Interface:  iface,
	}, true
}
