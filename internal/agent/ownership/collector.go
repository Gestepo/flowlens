package ownership

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	dockerinventory "flowlens/internal/agent/docker"
	flowebpf "flowlens/internal/agent/ebpf"
	processlookup "flowlens/internal/agent/process"
	"flowlens/internal/model"
)

const maxActiveConnections = 4096

type ProcessLookup interface {
	Lookup(uint32) (processlookup.Process, bool)
}

type Attributor struct {
	processes ProcessLookup
	mu        sync.RWMutex
	byCgroup  map[uint64]dockerinventory.Container
}

func NewAttributor(processes ProcessLookup) *Attributor {
	return &Attributor{processes: processes, byCgroup: make(map[uint64]dockerinventory.Container)}
}

func (attributor *Attributor) SetSnapshot(snapshot dockerinventory.Snapshot) {
	copyByCgroup := make(map[uint64]dockerinventory.Container, len(snapshot.ByCgroup))
	for id, container := range snapshot.ByCgroup {
		copyByCgroup[id] = container
	}
	attributor.mu.Lock()
	attributor.byCgroup = copyByCgroup
	attributor.mu.Unlock()
}

func (attributor *Attributor) Attribute(observation flowebpf.Observation) model.ConnectionDelta {
	owner := model.OwnerRef{Kind: model.OwnerHost}
	attributor.mu.RLock()
	container, found := attributor.byCgroup[observation.CgroupID]
	attributor.mu.RUnlock()
	if found {
		owner = model.OwnerRef{Kind: model.OwnerContainer, ContainerID: container.ID, ContainerName: container.Name}
	} else if process, ok := attributor.processes.Lookup(observation.PID); ok {
		owner = model.OwnerRef{Kind: model.OwnerProcess, PID: int(process.PID), Process: process.Name}
	}
	return model.ConnectionDelta{
		Protocol: protocolName(observation.Protocol),
		Local: model.Endpoint{
			IP: observation.LocalIP.String(), Port: observation.LocalPort, Confidence: model.ConfidenceIPOnly,
		},
		Remote: model.Endpoint{
			IP: observation.RemoteIP.String(), Port: observation.RemotePort, Confidence: model.ConfidenceIPOnly,
		},
		Owner:         owner,
		BytesSent:     clampUint64(observation.Sent),
		BytesReceived: clampUint64(observation.Received),
		State:         stateName(observation.State),
	}
}

type connectionKey struct {
	Protocol string
	Local    model.Endpoint
	Remote   model.Endpoint
	Owner    model.OwnerRef
	State    string
}

type Collector struct {
	attributor *Attributor
	mu         sync.Mutex
	values     map[connectionKey]model.ConnectionDelta
	dropped    uint64
}

func NewCollector(attributor *Attributor) *Collector {
	return &Collector{attributor: attributor, values: make(map[connectionKey]model.ConnectionDelta)}
}

func (collector *Collector) Observe(observation flowebpf.Observation) {
	if observation.LocalPort == 0 || observation.RemotePort == 0 || !observation.LocalIP.IsValid() || !observation.RemoteIP.IsValid() {
		collector.mu.Lock()
		collector.dropped++
		collector.mu.Unlock()
		return
	}
	connection := collector.attributor.Attribute(observation)
	key := connectionKey{Protocol: connection.Protocol, Local: connection.Local, Remote: connection.Remote, Owner: connection.Owner, State: connection.State}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	current, found := collector.values[key]
	if !found && len(collector.values) >= maxActiveConnections {
		collector.dropped++
		return
	}
	connection.BytesSent = saturatingAdd(current.BytesSent, connection.BytesSent)
	connection.BytesReceived = saturatingAdd(current.BytesReceived, connection.BytesReceived)
	collector.values[key] = connection
}

func (collector *Collector) Drain(observedAt time.Time) []model.Event {
	collector.mu.Lock()
	values := collector.values
	collector.values = make(map[connectionKey]model.ConnectionDelta)
	collector.mu.Unlock()

	events := make([]model.Event, 0, len(values))
	for key, connection := range values {
		connection := connection
		digest := sha256.Sum256([]byte(fmt.Sprintf("%v:%d", key, observedAt.UnixNano())))
		events = append(events, model.Event{
			ID:         fmt.Sprintf("connection:%x", digest[:12]),
			ObservedAt: observedAt.UTC(),
			Kind:       model.EventConnection,
			Connection: &connection,
		})
	}
	return events
}

func InventoryEvents(snapshot dockerinventory.Snapshot, observedAt time.Time) []model.Event {
	ids := make([]string, 0, len(snapshot.ByID))
	for id := range snapshot.ByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	events := make([]model.Event, 0, len(ids))
	for _, id := range ids {
		container := snapshot.ByID[id]
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, observedAt.UnixNano())))
		events = append(events, model.Event{
			ID:         fmt.Sprintf("owner:%x", digest[:12]),
			ObservedAt: observedAt.UTC(),
			Kind:       model.EventOwnerInventory,
			OwnerInventory: &model.OwnerInventory{
				Owner:     model.OwnerRef{Kind: model.OwnerContainer, ContainerID: container.ID, ContainerName: container.Name},
				CgroupID:  container.CgroupID,
				Addresses: append([]string(nil), container.Addresses...),
				Ports:     append([]uint16(nil), container.Ports...),
				Running:   container.Running,
			},
		})
	}
	return events
}

func protocolName(protocol uint8) string {
	switch protocol {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return strconv.Itoa(int(protocol))
	}
}

func stateName(state uint8) string {
	switch state {
	case 1:
		return "established"
	case 7:
		return "closed"
	case 10:
		return "listening"
	default:
		return strconv.Itoa(int(state))
	}
}

func clampUint64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func saturatingAdd(left, right int64) int64 {
	if right > math.MaxInt64-left {
		return math.MaxInt64
	}
	return left + right
}
