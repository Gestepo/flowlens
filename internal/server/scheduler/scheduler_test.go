package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOnlyOneSchedulerRunsNamedJobAtDueTime(t *testing.T) {
	store := newMemoryLeaseStore()
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	var runs atomic.Int32
	job := Job{Name: "hour-rollup", Interval: time.Hour, Run: func(context.Context) error { runs.Add(1); return nil }}
	one := New(store, "one", func() time.Time { return now })
	two := New(store, "two", func() time.Time { return now })
	var workers sync.WaitGroup
	for _, service := range []*Scheduler{one, two} {
		workers.Add(1)
		go func() { defer workers.Done(); _, _ = service.RunDue(context.Background(), job) }()
	}
	workers.Wait()
	require.Equal(t, int32(1), runs.Load())
}

func TestExpiredLeaseCanBeReclaimedAfterRestart(t *testing.T) {
	store := newMemoryLeaseStore()
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	store.leases["cleanup"] = lease{owner: "dead-process", expires: now.Add(-time.Second), nextRun: now.Add(-time.Hour)}
	runs := 0
	service := New(store, "replacement", func() time.Time { return now })
	worked, err := service.RunDue(context.Background(), Job{Name: "cleanup", Interval: time.Hour, Run: func(context.Context) error { runs++; return nil }})
	require.NoError(t, err)
	require.True(t, worked)
	require.Equal(t, 1, runs)
	require.Equal(t, now.Add(time.Hour), store.leases["cleanup"].nextRun)
}

func TestCancellationReleasesJobLease(t *testing.T) {
	store := newMemoryLeaseStore()
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := New(store, "worker", func() time.Time { return now })
	worked, err := service.RunDue(ctx, Job{Name: "cleanup", Interval: time.Hour, Run: func(ctx context.Context) error { return ctx.Err() }})
	require.True(t, worked)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, store.leases["cleanup"].owner)
}

func TestJobCanPersistAnExactNextSchedule(t *testing.T) {
	store := newMemoryLeaseStore()
	now := time.Date(2026, 7, 15, 9, 5, 0, 0, time.UTC)
	service := New(store, "worker", func() time.Time { return now })
	job := Job{Name: "hour-rollup", Interval: time.Hour, NextRun: func(time.Time) time.Time {
		return time.Date(2026, 7, 15, 9, 10, 0, 0, time.UTC)
	}, Run: func(context.Context) error { return nil }}
	worked, err := service.RunDue(context.Background(), job)
	require.NoError(t, err)
	require.True(t, worked)
	require.Equal(t, time.Date(2026, 7, 15, 9, 10, 0, 0, time.UTC), store.leases["hour-rollup"].nextRun)
}

type lease struct {
	owner            string
	expires, nextRun time.Time
	lastSuccess      *time.Time
	lastError        string
}
type memoryLeaseStore struct {
	mu     sync.Mutex
	leases map[string]lease
}

func newMemoryLeaseStore() *memoryLeaseStore {
	return &memoryLeaseStore{leases: make(map[string]lease)}
}
func (store *memoryLeaseStore) Claim(_ context.Context, name, owner string, now time.Time, duration time.Duration) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.leases[name]
	if !item.nextRun.IsZero() && item.nextRun.After(now) {
		return false, nil
	}
	if item.owner != "" && item.expires.After(now) {
		return false, nil
	}
	item.owner, item.expires = owner, now.Add(duration)
	store.leases[name] = item
	return true, nil
}
func (store *memoryLeaseStore) Heartbeat(_ context.Context, name, owner string, expires time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.leases[name]
	if item.owner == owner {
		item.expires = expires
		store.leases[name] = item
	}
	return nil
}
func (store *memoryLeaseStore) Finish(_ context.Context, name, owner string, success bool, at, next time.Time, message string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.leases[name]
	if item.owner == owner {
		item.owner = ""
		item.expires = time.Time{}
		item.nextRun = next
		item.lastError = message
		if success {
			item.lastSuccess = &at
		}
		store.leases[name] = item
	}
	return nil
}
