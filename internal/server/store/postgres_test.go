package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"flowlens/internal/model"
	"flowlens/internal/server/store"
	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestInsertBatchIsIdempotent(t *testing.T) {
	pool := openTestDatabase(t)
	trafficStore := store.New(pool)
	batch := fixtureBatch("batch-1", "event-1", 4096)

	inserted, err := trafficStore.InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = trafficStore.InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.False(t, inserted)
	require.Equal(t, int64(4096), sumBytes(t, pool))
}

func TestInsertBatchStoresHealthEvent(t *testing.T) {
	pool := openTestDatabase(t)
	now := time.Date(2026, 7, 14, 8, 2, 0, 0, time.UTC)
	batch := model.Batch{
		SchemaVersion: 1,
		BatchID:       "health-batch",
		NodeID:        "flowlens-node-1",
		SentAt:        now,
		Events: []model.Event{{
			ID:         "health-event",
			ObservedAt: now,
			Kind:       model.EventHealth,
			Health: &model.HealthEvent{
				Collector:     "spool",
				Status:        "degraded",
				Code:          "data_gap",
				DroppedEvents: 12,
				UsagePercent:  87.5,
				Message:       "detail limit reached",
			},
		}},
	}

	inserted, err := store.New(pool).InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.True(t, inserted)

	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM collector_health
		WHERE event_id = 'health-event' AND dropped_events = 12 AND usage_percent = 87.5
	`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestInsertBatchAggregatesEventsIntoMinuteBucket(t *testing.T) {
	pool := openTestDatabase(t)
	batch := fixtureBatch("aggregate-batch", "aggregate-event-1", 100)
	batch.Events[0].Packets = 2
	second := batch.Events[0]
	second.ID = "aggregate-event-2"
	second.ObservedAt = second.ObservedAt.Add(20 * time.Second)
	second.Bytes = 200
	second.Packets = 3
	batch.Events = append(batch.Events, second)

	inserted, err := store.New(pool).InsertBatch(context.Background(), batch)
	require.NoError(t, err)
	require.True(t, inserted)

	var bytes, packets int64
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT bytes, packets FROM traffic_minute
		WHERE node_id = 'flowlens-node-1' AND direction = 'inbound'
	`).Scan(&bytes, &packets))
	require.Equal(t, int64(300), bytes)
	require.Equal(t, int64(5), packets)
}

func TestInsertBatchDoesNotAggregateDuplicateEventFromNewBatch(t *testing.T) {
	pool := openTestDatabase(t)
	trafficStore := store.New(pool)
	first := fixtureBatch("batch-1", "shared-event", 4096)
	second := fixtureBatch("batch-2", "shared-event", 4096)

	inserted, err := trafficStore.InsertBatch(context.Background(), first)
	require.NoError(t, err)
	require.True(t, inserted)
	inserted, err = trafficStore.InsertBatch(context.Background(), second)
	require.NoError(t, err)
	require.True(t, inserted)

	require.Equal(t, int64(4096), sumBytes(t, pool))
	require.Equal(t, int64(4096), sumMinuteBytes(t, pool))
}

func openTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLOWLENS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLOWLENS_TEST_DATABASE_URL is not configured")
	}

	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, migrations.Apply(context.Background(), pool))
	_, err = pool.Exec(context.Background(), "TRUNCATE collector_health, interface_deltas, traffic_minute, ingest_batches, nodes CASCADE")
	require.NoError(t, err)
	return pool
}

func fixtureBatch(batchID, eventID string, bytes int64) model.Batch {
	now := time.Date(2026, 7, 14, 8, 1, 30, 0, time.UTC)
	return model.Batch{
		SchemaVersion: 1,
		BatchID:       batchID,
		NodeID:        "flowlens-node-1",
		SentAt:        now,
		Events: []model.Event{{
			ID:         eventID,
			ObservedAt: now,
			Kind:       model.EventInterfaceDelta,
			Direction:  model.DirectionInbound,
			Bytes:      bytes,
			Packets:    4,
			Interface:  "enp0s6",
		}},
	}
}

func sumBytes(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var total int64
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT COALESCE(sum(bytes), 0) FROM interface_deltas").Scan(&total))
	return total
}

func sumMinuteBytes(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var total int64
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT COALESCE(sum(bytes), 0) FROM traffic_minute").Scan(&total))
	return total
}
