package spool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestSpoolReplaysFIFOAndRemovesOnlyAcknowledgedItem(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	queue, err := New(t.TempDir(), WithClock(clock))
	require.NoError(t, err)

	_, err = queue.Enqueue(interfaceBatch("batch-1", now, 1))
	require.NoError(t, err)
	now = now.Add(time.Second)
	_, err = queue.Enqueue(interfaceBatch("batch-2", now, 1))
	require.NoError(t, err)

	first, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "batch-1", first.Batch.BatchID)
	require.NoError(t, queue.Ack(first))

	second, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "batch-2", second.Batch.BatchID)
}

func TestSpoolUsesAtomicFiles(t *testing.T) {
	dir := t.TempDir()
	queue, err := New(dir)
	require.NoError(t, err)

	_, err = queue.Enqueue(interfaceBatch("batch", time.Now().UTC(), 1))
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, ".json.gz", filepath.Ext(strings.TrimSuffix(entries[0].Name(), ".gz"))+filepath.Ext(entries[0].Name()))
	require.NotContains(t, entries[0].Name(), ".tmp")
}

func TestSpoolEvictsExpiredDetailAndReportsDataGap(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	queue, err := New(t.TempDir(), WithClock(func() time.Time { return now }), WithMaxAge(30*time.Minute))
	require.NoError(t, err)
	_, err = queue.Enqueue(detailBatch("expired", now))
	require.NoError(t, err)
	now = now.Add(31 * time.Minute)

	report, err := queue.Enqueue(detailBatch("current", now))
	require.NoError(t, err)
	require.Equal(t, "data_gap", report.Code)
	require.Equal(t, int64(1), report.DroppedBatches)
	require.Equal(t, int64(1), report.DroppedEvents)

	item, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "current", item.Batch.BatchID)
}

func TestSpoolKeepsNewestBatchWhenOverSizeLimit(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	queue, err := New(t.TempDir(), WithClock(func() time.Time { return now }), WithMaxBytes(1))
	require.NoError(t, err)
	_, err = queue.Enqueue(detailBatch("old", now))
	require.NoError(t, err)
	now = now.Add(time.Second)

	report, err := queue.Enqueue(detailBatch("new", now))
	require.NoError(t, err)
	require.Equal(t, int64(1), report.DroppedBatches)

	item, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "new", item.Batch.BatchID)
}

func TestSpoolCompactsExpiredInterfaceSamplesAndPreservesMinuteTotals(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 5, 0, time.UTC)
	queue, err := New(t.TempDir(), WithClock(func() time.Time { return now }), WithMaxAge(30*time.Minute))
	require.NoError(t, err)
	_, err = queue.Enqueue(interfaceBatch("sample-1", now, 10))
	require.NoError(t, err)
	now = now.Add(10 * time.Second)
	_, err = queue.Enqueue(interfaceBatch("sample-2", now, 15))
	require.NoError(t, err)
	now = now.Add(31 * time.Minute)

	report, err := queue.Enqueue(detailBatch("current", now))
	require.NoError(t, err)
	require.Zero(t, report.DroppedBatches)

	item, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, item.Batch.BatchID, "minute-summary")
	require.Len(t, item.Batch.Events, 1)
	require.Equal(t, int64(25), item.Batch.Events[0].Bytes)
	require.Equal(t, time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC), item.Batch.Events[0].ObservedAt)
}

func TestSpoolReportsPressureFromOldestQueuedBatchAge(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	queue, err := New(t.TempDir(), WithClock(func() time.Time { return now }), WithMaxAge(30*time.Minute))
	require.NoError(t, err)
	_, err = queue.Enqueue(detailBatch("old", now))
	require.NoError(t, err)
	now = now.Add(24 * time.Minute)
	report, err := queue.Enqueue(detailBatch("current", now))
	require.NoError(t, err)
	require.InDelta(t, 80, report.UsagePercent, 0.1)
}

func TestSpoolDropsExpiredDetailBeforeProtectedInterfaceData(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	queue, err := New(t.TempDir(), WithClock(func() time.Time { return now }), WithMaxAge(30*time.Minute))
	require.NoError(t, err)
	_, err = queue.Enqueue(interfaceBatch("traffic", now, 20))
	require.NoError(t, err)
	_, err = queue.Enqueue(detailBatch("detail", now))
	require.NoError(t, err)
	now = now.Add(31 * time.Minute)

	report, err := queue.Enqueue(detailBatch("current", now))
	require.NoError(t, err)
	require.Equal(t, int64(1), report.DroppedBatches)
	require.Equal(t, int64(1), report.DroppedEvents)
	item, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, item.Batch.BatchID, "minute-summary")
}

func TestSpoolRecoversAfterSummaryWasPublishedBeforeOriginalWasRemoved(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	queue, err := New(dir, WithClock(func() time.Time { return now }), WithMaxAge(30*time.Minute))
	require.NoError(t, err)
	originalAt := now
	original := interfaceBatch("already-compacted", originalAt, 25)
	_, err = queue.Enqueue(original)
	require.NoError(t, err)
	now = now.Add(31 * time.Minute)
	minute := originalAt.Truncate(time.Minute)
	summary := model.Batch{
		SchemaVersion: 1, BatchID: minuteSummaryBatchID(original.NodeID, minute), NodeID: original.NodeID, SentAt: minute,
		CompactedBatchIDs: []string{original.BatchID},
		Events:            []model.Event{mergeInterfaceEvent(nil, original.Events[0], minute)[0]},
	}
	require.NoError(t, writeBatchFile(dir, filepath.Join(dir, batchFileName(summary.BatchID, minute)), summary, minute))

	_, err = queue.Enqueue(detailBatch("current", now))
	require.NoError(t, err)
	item, ok, err := queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, summary.BatchID, item.Batch.BatchID)
	require.Equal(t, int64(25), item.Batch.Events[0].Bytes)
	require.Equal(t, []string{"already-compacted"}, item.Batch.CompactedBatchIDs)
	require.NoError(t, queue.Ack(item))
	item, ok, err = queue.Peek()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "current", item.Batch.BatchID)
}

func interfaceBatch(id string, at time.Time, bytes int64) model.Batch {
	return model.Batch{
		SchemaVersion: 1,
		BatchID:       id,
		NodeID:        "flowlens-node-1",
		SentAt:        at,
		Events: []model.Event{{
			ID:         id + "-event",
			ObservedAt: at,
			Kind:       model.EventInterfaceDelta,
			Direction:  model.DirectionInbound,
			Bytes:      bytes,
			Interface:  "enp0s6",
		}},
	}
}

func detailBatch(id string, at time.Time) model.Batch {
	return model.Batch{
		SchemaVersion: 1,
		BatchID:       id,
		NodeID:        "flowlens-node-1",
		SentAt:        at,
		Events: []model.Event{{
			ID: id + "-event", ObservedAt: at, Kind: model.EventConnection,
			Connection: &model.ConnectionDelta{
				Protocol: "tcp", Local: model.Endpoint{IP: "10.0.0.2", Port: 42000, Confidence: model.ConfidenceIPOnly},
				Remote: model.Endpoint{IP: "203.0.113.10", Port: 443, Confidence: model.ConfidenceIPOnly}, Owner: model.OwnerRef{Kind: model.OwnerHost},
			},
		}},
	}
}
