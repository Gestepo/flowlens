package model

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchValidateAcceptsInterfaceDelta(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	batch := Batch{
		SchemaVersion: 1,
		BatchID:       "01JFLOWLENS00000000000001",
		NodeID:        "flowlens-node-1",
		SentAt:        now,
		Events: []Event{{
			ID:         "01JFLOWLENS00000000000002",
			ObservedAt: now,
			Kind:       EventInterfaceDelta,
			Direction:  DirectionInbound,
			Bytes:      2048,
			Interface:  "enp0s6",
		}},
	}

	require.NoError(t, batch.Validate())
}

func TestBatchValidateRejectsNegativeBytes(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	batch := Batch{
		SchemaVersion: 1,
		BatchID:       "batch",
		NodeID:        "node",
		SentAt:        now,
		Events: []Event{{
			ID:         "event",
			ObservedAt: now,
			Kind:       EventInterfaceDelta,
			Direction:  DirectionOutbound,
			Bytes:      -1,
			Interface:  "enp0s6",
		}},
	}

	require.ErrorContains(t, batch.Validate(), "bytes")
}

func TestBatchValidateAcceptsHealthEventWithoutDirection(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	batch := Batch{
		SchemaVersion: 1,
		BatchID:       "batch",
		NodeID:        "node",
		SentAt:        now,
		Events: []Event{{
			ID:         "health-event",
			ObservedAt: now,
			Kind:       EventHealth,
			Health: &HealthEvent{
				Collector:     "spool",
				Status:        "degraded",
				Code:          "data_gap",
				DroppedEvents: 12,
			},
		}},
	}

	require.NoError(t, batch.Validate())
}

func TestBatchValidateRejectsInvalidEnvelope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Batch)
		want   string
	}{
		{name: "schema version", mutate: func(b *Batch) { b.SchemaVersion = 2 }, want: "schema_version"},
		{name: "batch id", mutate: func(b *Batch) { b.BatchID = "" }, want: "batch_id"},
		{name: "node id", mutate: func(b *Batch) { b.NodeID = "" }, want: "node_id"},
		{name: "sent at", mutate: func(b *Batch) { b.SentAt = time.Time{} }, want: "sent_at"},
		{name: "empty events", mutate: func(b *Batch) { b.Events = nil }, want: "event count"},
		{name: "too many events", mutate: func(b *Batch) {
			b.Events = make([]Event, 5001)
		}, want: "event count"},
		{name: "long node id", mutate: func(b *Batch) { b.NodeID = strings.Repeat("n", 129) }, want: "node_id"},
		{name: "empty compacted batch id", mutate: func(b *Batch) { b.CompactedBatchIDs = []string{""} }, want: "compacted batch id"},
		{name: "duplicate compacted batch id", mutate: func(b *Batch) { b.CompactedBatchIDs = []string{"source", "source"} }, want: "unique"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := validInterfaceBatch()
			test.mutate(&batch)
			require.ErrorContains(t, batch.Validate(), test.want)
		})
	}
}

func TestBatchValidateRejectsInvalidEvent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Event)
		want   string
	}{
		{name: "event id", mutate: func(e *Event) { e.ID = "" }, want: "event 0 id"},
		{name: "observed at", mutate: func(e *Event) { e.ObservedAt = time.Time{} }, want: "observed_at"},
		{name: "kind", mutate: func(e *Event) { e.Kind = "unknown" }, want: "kind"},
		{name: "direction", mutate: func(e *Event) { e.Direction = "" }, want: "direction"},
		{name: "interface", mutate: func(e *Event) { e.Interface = "" }, want: "interface"},
		{name: "packets", mutate: func(e *Event) { e.Packets = -1 }, want: "packets"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := validInterfaceBatch()
			test.mutate(&batch.Events[0])
			require.ErrorContains(t, batch.Validate(), test.want)
		})
	}
}

func TestBatchValidateRejectsInvalidHealthPayload(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	batch := Batch{SchemaVersion: 1, BatchID: "batch", NodeID: "node", SentAt: now, Events: []Event{{ID: "health", ObservedAt: now, Kind: EventHealth}}}

	require.ErrorContains(t, batch.Validate(), "health")
	batch.Events[0].Health = &HealthEvent{Collector: "spool", Status: "degraded", Code: "buffer_pressure", UsagePercent: 101}
	require.ErrorContains(t, batch.Validate(), "health")
}

func validInterfaceBatch() Batch {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	return Batch{
		SchemaVersion: 1,
		BatchID:       "batch",
		NodeID:        "node",
		SentAt:        now,
		Events: []Event{{
			ID:         "event",
			ObservedAt: now,
			Kind:       EventInterfaceDelta,
			Direction:  DirectionInbound,
			Bytes:      1,
			Packets:    1,
			Interface:  "enp0s6",
		}},
	}
}
