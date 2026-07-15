package scheduler

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

const leaseDuration = 2 * time.Minute

type Store interface {
	Claim(context.Context, string, string, time.Time, time.Duration) (bool, error)
	Heartbeat(context.Context, string, string, time.Time) error
	Finish(context.Context, string, string, bool, time.Time, time.Time, string) error
}

type Job struct {
	Name     string
	Interval time.Duration
	NextRun  func(time.Time) time.Time
	Run      func(context.Context) error
}

type Scheduler struct {
	store Store
	owner string
	now   func() time.Time
}

func New(store Store, owner string, now func() time.Time) *Scheduler {
	return &Scheduler{store: store, owner: owner, now: now}
}

func (scheduler *Scheduler) RunDue(ctx context.Context, job Job) (bool, error) {
	started := scheduler.now()
	claimed, err := scheduler.store.Claim(ctx, job.Name, scheduler.owner, started, leaseDuration)
	if err != nil || !claimed {
		return claimed, err
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	defer stopHeartbeat()
	go scheduler.heartbeat(heartbeatCtx, job.Name)
	runErr := job.Run(ctx)
	finished := scheduler.now()
	next := finished.Add(job.Interval)
	if job.NextRun != nil {
		next = job.NextRun(finished)
	}
	if runErr != nil {
		retry := job.Interval
		if retry <= 0 || retry > time.Minute {
			retry = time.Minute
		}
		next = finished.Add(retry)
		if errors.Is(runErr, context.Canceled) {
			next = finished
		}
	}
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := scheduler.store.Finish(finishCtx, job.Name, scheduler.owner, runErr == nil, finished, next, message); err != nil && runErr == nil {
		return true, err
	}
	return true, runErr
}

func (scheduler *Scheduler) Run(ctx context.Context, jobs []Job) error {
	spread := time.Duration(rand.Int64N(int64(5*time.Second) + 1))
	timer := time.NewTimer(spread)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		for _, job := range jobs {
			_, _ = scheduler.RunDue(ctx, job)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (scheduler *Scheduler) heartbeat(ctx context.Context, name string) {
	ticker := time.NewTicker(leaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeatCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = scheduler.store.Heartbeat(heartbeatCtx, name, scheduler.owner, scheduler.now().Add(leaseDuration))
			cancel()
		}
	}
}
