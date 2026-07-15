package model

import (
	"fmt"
	"time"
)

type EventKind string

type Direction string

const (
	EventInterfaceDelta EventKind = "interface_delta"
	EventHealth         EventKind = "health"
	EventConnection     EventKind = "connection_delta"
	EventProxyRequest   EventKind = "proxy_request"
	EventNameEvidence   EventKind = "name_evidence"
	EventOwnerInventory EventKind = "owner_inventory"

	DirectionInbound   Direction = "inbound"
	DirectionOutbound  Direction = "outbound"
	DirectionInternal  Direction = "internal"
	DirectionContainer Direction = "container_to_container"
)

type Event struct {
	ID             string           `json:"id"`
	ObservedAt     time.Time        `json:"observed_at"`
	Kind           EventKind        `json:"kind"`
	Direction      Direction        `json:"direction"`
	Bytes          int64            `json:"bytes"`
	Packets        int64            `json:"packets,omitempty"`
	Interface      string           `json:"interface,omitempty"`
	Health         *HealthEvent     `json:"health,omitempty"`
	Connection     *ConnectionDelta `json:"connection,omitempty"`
	ProxyRequest   *ProxyRequest    `json:"proxy_request,omitempty"`
	NameEvidence   *NameEvidence    `json:"name_evidence,omitempty"`
	OwnerInventory *OwnerInventory  `json:"owner_inventory,omitempty"`
}

type HealthEvent struct {
	Collector     string  `json:"collector"`
	Status        string  `json:"status"`
	Code          string  `json:"code"`
	DroppedEvents int64   `json:"dropped_events,omitempty"`
	UsagePercent  float64 `json:"usage_percent,omitempty"`
	Message       string  `json:"message,omitempty"`
}

type Batch struct {
	SchemaVersion     int       `json:"schema_version"`
	BatchID           string    `json:"batch_id"`
	NodeID            string    `json:"node_id"`
	SentAt            time.Time `json:"sent_at"`
	Events            []Event   `json:"events"`
	CompactedBatchIDs []string  `json:"compacted_batch_ids,omitempty"`
}

func (b Batch) Validate() error {
	if b.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1")
	}
	if b.BatchID == "" || len(b.BatchID) > 128 {
		return fmt.Errorf("batch_id must contain 1..128 bytes")
	}
	if b.NodeID == "" || len(b.NodeID) > 128 {
		return fmt.Errorf("node_id must contain 1..128 bytes")
	}
	if b.SentAt.IsZero() {
		return fmt.Errorf("sent_at is required")
	}
	if len(b.Events) == 0 || len(b.Events) > 5000 {
		return fmt.Errorf("event count must be within 1..5000")
	}
	if len(b.CompactedBatchIDs) > 5000 {
		return fmt.Errorf("compacted batch count must not exceed 5000")
	}
	seenCompacted := make(map[string]struct{}, len(b.CompactedBatchIDs))
	for _, id := range b.CompactedBatchIDs {
		if id == "" || len(id) > 128 {
			return fmt.Errorf("compacted batch id must contain 1..128 bytes")
		}
		if _, exists := seenCompacted[id]; exists {
			return fmt.Errorf("compacted batch ids must be unique")
		}
		seenCompacted[id] = struct{}{}
	}

	for i, event := range b.Events {
		if event.ID == "" || len(event.ID) > 128 {
			return fmt.Errorf("event %d id must contain 1..128 bytes", i)
		}
		if event.ObservedAt.IsZero() {
			return fmt.Errorf("event %d observed_at is required", i)
		}
		if event.Bytes < 0 {
			return fmt.Errorf("event %d bytes must be non-negative", i)
		}
		if event.Packets < 0 {
			return fmt.Errorf("event %d packets must be non-negative", i)
		}

		switch event.Kind {
		case EventInterfaceDelta:
			if event.nonHealthPayloadCount() != 0 {
				return fmt.Errorf("event %d must contain exactly one payload matching its kind", i)
			}
			if !event.Direction.valid() {
				return fmt.Errorf("event %d direction is invalid", i)
			}
			if event.Interface == "" {
				return fmt.Errorf("event %d interface is required", i)
			}
		case EventHealth:
			if event.Health == nil || event.Health.Collector == "" || event.Health.Status == "" || event.Health.Code == "" || event.Health.DroppedEvents < 0 || event.Health.UsagePercent < 0 || event.Health.UsagePercent > 100 {
				return fmt.Errorf("event %d health payload is invalid", i)
			}
			if event.trafficPayloadCount() != 0 {
				return fmt.Errorf("event %d must contain exactly one payload matching its kind", i)
			}
		case EventConnection:
			if event.Connection == nil || event.nonHealthPayloadCount() != 1 {
				return fmt.Errorf("event %d must contain exactly one payload matching its kind", i)
			}
			if err := event.Connection.validate(); err != nil {
				return fmt.Errorf("event %d connection: %w", i, err)
			}
		case EventProxyRequest:
			if event.ProxyRequest == nil || event.nonHealthPayloadCount() != 1 {
				return fmt.Errorf("event %d must contain exactly one payload matching its kind", i)
			}
			if err := event.ProxyRequest.validate(); err != nil {
				return fmt.Errorf("event %d proxy request: %w", i, err)
			}
		case EventNameEvidence:
			if event.NameEvidence == nil || event.nonHealthPayloadCount() != 1 {
				return fmt.Errorf("event %d must contain exactly one payload matching its kind", i)
			}
			if err := event.NameEvidence.validate(); err != nil {
				return fmt.Errorf("event %d name evidence: %w", i, err)
			}
		case EventOwnerInventory:
			if event.OwnerInventory == nil || event.nonHealthPayloadCount() != 1 {
				return fmt.Errorf("event %d must contain exactly one payload matching its kind", i)
			}
			if err := event.OwnerInventory.validate(); err != nil {
				return fmt.Errorf("event %d owner inventory: %w", i, err)
			}
		default:
			return fmt.Errorf("event %d kind is unsupported", i)
		}
	}
	return nil
}

func (event Event) trafficPayloadCount() int {
	count := 0
	for _, present := range []bool{event.Connection != nil, event.ProxyRequest != nil, event.NameEvidence != nil, event.OwnerInventory != nil} {
		if present {
			count++
		}
	}
	return count
}

func (event Event) nonHealthPayloadCount() int {
	count := event.trafficPayloadCount()
	if event.Health != nil {
		count++
	}
	return count
}

func (d Direction) valid() bool {
	switch d {
	case DirectionInbound, DirectionOutbound, DirectionInternal, DirectionContainer:
		return true
	default:
		return false
	}
}
