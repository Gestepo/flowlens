package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"flowlens/internal/agent/interfacecounter"
	"flowlens/internal/agent/spool"
	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestSampleEstablishesBaselineThenQueuesAndSendsDelta(t *testing.T) {
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{
		{"enp0s6": {RXBytes: 100, RXPackets: 1, TXBytes: 200, TXPackets: 2}},
		{"enp0s6": {RXBytes: 160, RXPackets: 2, TXBytes: 280, TXPackets: 4}},
	}}
	queue := &fakeQueue{}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", reader.Read, queue, delivery)

	require.NoError(t, agent.Sample(context.Background(), time.Unix(1, 0).UTC()))
	require.Empty(t, delivery.batches)
	require.NoError(t, agent.Sample(context.Background(), time.Unix(2, 0).UTC()))

	require.Len(t, delivery.batches, 1)
	require.Len(t, delivery.batches[0].Events, 2)
	require.Equal(t, int64(60), delivery.batches[0].Events[0].Bytes)
	require.Equal(t, int64(80), delivery.batches[0].Events[1].Bytes)
	require.Empty(t, queue.items)
}

func TestSampleKeepsBatchQueuedWhenDeliveryFails(t *testing.T) {
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{
		{"enp0s6": {RXBytes: 100}},
		{"enp0s6": {RXBytes: 200}},
	}}
	queue := &fakeQueue{}
	delivery := &fakeSender{err: errors.New("offline")}
	agent := New("flowlens-node-1", reader.Read, queue, delivery)
	require.NoError(t, agent.Sample(context.Background(), time.Unix(1, 0).UTC()))

	err := agent.Sample(context.Background(), time.Unix(2, 0).UTC())

	require.ErrorContains(t, err, "offline")
	require.Len(t, queue.items, 1)
}

func TestSampleContinuesPersistedCountersAcrossAgentRestart(t *testing.T) {
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{
		{"enp0s6": {RXBytes: 160, RXPackets: 2, TXBytes: 280, TXPackets: 4}},
	}}
	state := &fakeCounterState{checkpoint: interfacecounter.NewCheckpoint("flowlens-node-1", map[string]interfacecounter.Counter{
		"enp0s6": {RXBytes: 100, RXPackets: 1, TXBytes: 200, TXPackets: 2},
	}, nil), found: true}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", reader.Read, &fakeQueue{}, delivery, WithCounterState(state))

	require.NoError(t, agent.Sample(context.Background(), time.Unix(2, 0).UTC()))

	require.Len(t, delivery.batches, 1)
	require.Equal(t, int64(60), delivery.batches[0].Events[0].Bytes)
	require.Equal(t, int64(80), delivery.batches[0].Events[1].Bytes)
	require.Len(t, state.saved, 2)
	require.NotNil(t, state.saved[0].PendingBatch)
	require.Nil(t, state.saved[1].PendingBatch)
	require.Equal(t, uint64(160), state.saved[1].Counters["enp0s6"].RXBytes)
}

func TestSampleRecoversPendingCounterBatchBeforeNewDelta(t *testing.T) {
	pendingAt := time.Unix(2, 0).UTC()
	pending := model.Batch{SchemaVersion: 1, BatchID: "pending-batch", NodeID: "flowlens-node-1", SentAt: pendingAt, Events: []model.Event{{
		ID: "pending-event", ObservedAt: pendingAt, Kind: model.EventInterfaceDelta, Direction: model.DirectionInbound, Interface: "enp0s6", Bytes: 60, Packets: 1,
	}}}
	state := &fakeCounterState{checkpoint: interfacecounter.NewCheckpoint("flowlens-node-1", map[string]interfacecounter.Counter{"enp0s6": {RXBytes: 160, RXPackets: 2}}, &pending), found: true}
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{{"enp0s6": {RXBytes: 200, RXPackets: 3}}}}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", reader.Read, &fakeQueue{}, delivery, WithCounterState(state))

	require.NoError(t, agent.Sample(context.Background(), time.Unix(3, 0).UTC()))

	require.Len(t, delivery.batches, 2)
	require.Equal(t, "pending-batch", delivery.batches[0].BatchID)
	require.Equal(t, int64(40), delivery.batches[1].Events[0].Bytes)
	require.Nil(t, state.saved[len(state.saved)-1].PendingBatch)
}

func TestSampleRecoversCheckpointWhenQueueWriteFails(t *testing.T) {
	state := &fakeCounterState{checkpoint: interfacecounter.NewCheckpoint("flowlens-node-1", map[string]interfacecounter.Counter{"enp0s6": {RXBytes: 100, RXPackets: 1}}, nil), found: true}
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{
		{"enp0s6": {RXBytes: 160, RXPackets: 2}},
		{"enp0s6": {RXBytes: 200, RXPackets: 3}},
	}}
	queue := &fakeQueue{err: errors.New("disk unavailable")}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", reader.Read, queue, delivery, WithCounterState(state))

	require.ErrorContains(t, agent.Sample(context.Background(), time.Unix(2, 0).UTC()), "disk unavailable")
	require.NotNil(t, state.checkpoint.PendingBatch)
	queue.err = nil
	require.NoError(t, agent.Sample(context.Background(), time.Unix(3, 0).UTC()))

	require.Len(t, delivery.batches, 2)
	require.Equal(t, int64(60), delivery.batches[0].Events[0].Bytes)
	require.Equal(t, int64(40), delivery.batches[1].Events[0].Bytes)
	require.Nil(t, state.checkpoint.PendingBatch)
}

func TestCounterCheckpointCrashPhasesUseStableDurableBatchIDs(t *testing.T) {
	for _, test := range []struct {
		name           string
		pendingInState bool
		pendingInSpool bool
		wantDeliveries int
	}{
		{name: "before enqueue", pendingInState: true, wantDeliveries: 2},
		{name: "after enqueue", pendingInState: true, pendingInSpool: true, wantDeliveries: 3},
		{name: "after checkpoint clear", pendingInSpool: true, wantDeliveries: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			queue, err := spool.New(directory + "/spool")
			require.NoError(t, err)
			state := interfacecounter.NewStateFile(directory + "/interface-counters.json")
			pendingAt := time.Unix(2, 0).UTC()
			pending := model.Batch{SchemaVersion: 1, BatchID: "stable-pending-batch", NodeID: "flowlens-node-1", SentAt: pendingAt, Events: []model.Event{{
				ID: "stable-pending-event", ObservedAt: pendingAt, Kind: model.EventInterfaceDelta, Direction: model.DirectionInbound, Interface: "enp0s6", Bytes: 60, Packets: 1,
			}}}
			var pendingState *model.Batch
			if test.pendingInState {
				pendingState = &pending
			}
			require.NoError(t, state.Save(interfacecounter.NewCheckpoint("flowlens-node-1", map[string]interfacecounter.Counter{"enp0s6": {RXBytes: 160, RXPackets: 2}}, pendingState)))
			if test.pendingInSpool {
				_, err := queue.Enqueue(pending)
				require.NoError(t, err)
			}
			reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{{"enp0s6": {RXBytes: 200, RXPackets: 3}}}}
			delivery := &fakeSender{}
			restarted := New("flowlens-node-1", reader.Read, queue, delivery, WithCounterState(state))

			require.NoError(t, restarted.Sample(context.Background(), time.Unix(3, 0).UTC()))
			require.Len(t, delivery.batches, test.wantDeliveries)
			uniqueBytes := map[string]int64{}
			for _, batch := range delivery.batches {
				var batchBytes int64
				for _, event := range batch.Events {
					batchBytes += event.Bytes
				}
				if _, seen := uniqueBytes[batch.BatchID]; !seen {
					uniqueBytes[batch.BatchID] = batchBytes
				}
			}
			require.Equal(t, int64(60), uniqueBytes["stable-pending-batch"])
			var total int64
			for _, bytes := range uniqueBytes {
				total += bytes
			}
			require.Equal(t, int64(100), total)
		})
	}
}

func TestSampleTurnsEvictionReportIntoHealthBatch(t *testing.T) {
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{
		{"enp0s6": {RXBytes: 100}},
		{"enp0s6": {RXBytes: 200}},
	}}
	queue := &fakeQueue{nextReport: spool.DropReport{Code: "data_gap", DroppedBatches: 2, DroppedEvents: 7}}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", reader.Read, queue, delivery)
	require.NoError(t, agent.Sample(context.Background(), time.Unix(1, 0).UTC()))

	require.NoError(t, agent.Sample(context.Background(), time.Unix(2, 0).UTC()))

	require.Len(t, delivery.batches, 2)
	healthBatch := delivery.batches[1]
	require.Equal(t, model.EventHealth, healthBatch.Events[0].Kind)
	require.Equal(t, "data_gap", healthBatch.Events[0].Health.Code)
	require.Equal(t, int64(7), healthBatch.Events[0].Health.DroppedEvents)
}

func TestSampleReportsSpoolPressureAndRecoveryTransitions(t *testing.T) {
	reader := &counterReader{snapshots: []map[string]interfacecounter.Counter{
		{"enp0s6": {RXBytes: 100}}, {"enp0s6": {RXBytes: 200}}, {"enp0s6": {RXBytes: 300}},
	}}
	queue := &fakeQueue{}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", reader.Read, queue, delivery)
	require.NoError(t, agent.Sample(context.Background(), time.Unix(1, 0).UTC()))
	queue.nextReport = spool.DropReport{UsagePercent: 82}
	require.NoError(t, agent.Sample(context.Background(), time.Unix(2, 0).UTC()))
	require.Equal(t, "buffer_pressure", delivery.batches[1].Events[0].Health.Code)
	require.Equal(t, "degraded", delivery.batches[1].Events[0].Health.Status)
	require.Equal(t, float64(82), delivery.batches[1].Events[0].Health.UsagePercent)
	queue.nextReport = spool.DropReport{UsagePercent: 10}
	require.NoError(t, agent.Sample(context.Background(), time.Unix(3, 0).UTC()))
	require.Equal(t, "buffer_recovered", delivery.batches[3].Events[0].Health.Code)
	require.Equal(t, "healthy", delivery.batches[3].Events[0].Health.Status)
}

func TestRecordQueuesAndSendsDetailedEventsInBoundedBatches(t *testing.T) {
	queue := &fakeQueue{}
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", nil, queue, delivery)
	now := time.Unix(3, 0).UTC()
	events := make([]model.Event, 5001)
	for index := range events {
		connection := model.ConnectionDelta{
			Protocol: "tcp",
			Local:    model.Endpoint{IP: "10.0.0.2", Port: 42000, Confidence: model.ConfidenceIPOnly},
			Remote:   model.Endpoint{IP: "203.0.113.10", Port: 443, Confidence: model.ConfidenceIPOnly},
			Owner:    model.OwnerRef{Kind: model.OwnerHost},
		}
		events[index] = model.Event{ID: fmt.Sprintf("event-%d", index), ObservedAt: now, Kind: model.EventConnection, Connection: &connection}
	}

	require.NoError(t, agent.Record(context.Background(), now, events))
	require.Len(t, delivery.batches, 2)
	require.Len(t, delivery.batches[0].Events, 5000)
	require.Len(t, delivery.batches[1].Events, 1)
}

func TestRecordUsesDistinctBatchIDsForDifferentEventsAtSameTime(t *testing.T) {
	delivery := &fakeSender{}
	agent := New("flowlens-node-1", nil, &fakeQueue{}, delivery)
	now := time.Unix(4, 0).UTC()
	first := model.Event{ID: "evidence-a", ObservedAt: now, Kind: model.EventHealth, Health: &model.HealthEvent{Collector: "name_capture", Status: "healthy", Code: "active"}}
	second := model.Event{ID: "request-b", ObservedAt: now, Kind: model.EventHealth, Health: &model.HealthEvent{Collector: "npm_logs", Status: "healthy", Code: "active"}}

	require.NoError(t, agent.Record(context.Background(), now, []model.Event{first}))
	require.NoError(t, agent.Record(context.Background(), now, []model.Event{second}))
	require.Len(t, delivery.batches, 2)
	require.NotEqual(t, delivery.batches[0].BatchID, delivery.batches[1].BatchID)
}

type counterReader struct {
	snapshots []map[string]interfacecounter.Counter
	index     int
}

func (reader *counterReader) Read() (map[string]interfacecounter.Counter, error) {
	result := reader.snapshots[reader.index]
	reader.index++
	return result, nil
}

type fakeQueue struct {
	items      []spool.Item
	nextReport spool.DropReport
	err        error
}

func (queue *fakeQueue) Enqueue(batch model.Batch) (spool.DropReport, error) {
	if queue.err != nil {
		return spool.DropReport{}, queue.err
	}
	queue.items = append(queue.items, spool.Item{Batch: batch})
	report := queue.nextReport
	queue.nextReport = spool.DropReport{}
	return report, nil
}

func (queue *fakeQueue) Peek() (spool.Item, bool, error) {
	if len(queue.items) == 0 {
		return spool.Item{}, false, nil
	}
	return queue.items[0], true, nil
}

func (queue *fakeQueue) Ack(spool.Item) error {
	queue.items = queue.items[1:]
	return nil
}

type fakeSender struct {
	batches []model.Batch
	err     error
}

type fakeCounterState struct {
	checkpoint interfacecounter.Checkpoint
	found      bool
	saved      []interfacecounter.Checkpoint
}

func (state *fakeCounterState) Load() (interfacecounter.Checkpoint, bool, error) {
	return state.checkpoint, state.found, nil
}

func (state *fakeCounterState) Save(checkpoint interfacecounter.Checkpoint) error {
	state.checkpoint = checkpoint
	state.found = true
	state.saved = append(state.saved, checkpoint)
	return nil
}

func (sender *fakeSender) Send(_ context.Context, batch model.Batch) error {
	sender.batches = append(sender.batches, batch)
	return sender.err
}
