package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"flowlens/internal/agent/interfacecounter"
	"flowlens/internal/agent/spool"
	"flowlens/internal/model"
)

type CounterReader func() (map[string]interfacecounter.Counter, error)

type Queue interface {
	Enqueue(model.Batch) (spool.DropReport, error)
	Peek() (spool.Item, bool, error)
	Ack(spool.Item) error
}

type Sender interface {
	Send(context.Context, model.Batch) error
}

type CounterState interface {
	Load() (interfacecounter.Checkpoint, bool, error)
	Save(interfacecounter.Checkpoint) error
}

type Option func(*App)

func WithCounterState(state CounterState) Option { return func(app *App) { app.counterState = state } }

type App struct {
	nodeID         string
	read           CounterReader
	queue          Queue
	sender         Sender
	previous       map[string]interfacecounter.Counter
	counterState   CounterState
	stateLoaded    bool
	spoolPressured bool
}

func New(nodeID string, read CounterReader, queue Queue, sender Sender, options ...Option) *App {
	app := &App{nodeID: nodeID, read: read, queue: queue, sender: sender}
	for _, option := range options {
		option(app)
	}
	return app
}

func (app *App) Sample(ctx context.Context, observedAt time.Time) error {
	if err := app.recoverCounterState(observedAt); err != nil {
		return err
	}
	current, err := app.read()
	if err != nil {
		return fmt.Errorf("read interface counters: %w", err)
	}
	if app.previous == nil {
		if err := app.saveCounterState(current, nil); err != nil {
			return err
		}
		app.previous = current
		return app.drain(ctx)
	}

	events := interfacecounter.Delta(app.previous, current, interfacecounter.DefaultAllowed, observedAt)
	if len(events) == 0 {
		if err := app.saveCounterState(current, nil); err != nil {
			return err
		}
		app.previous = current
		return app.drain(ctx)
	}
	batch := model.Batch{
		SchemaVersion: 1,
		BatchID:       eventID("batch", app.nodeID, observedAt),
		NodeID:        app.nodeID,
		SentAt:        observedAt,
		Events:        events,
	}
	if err := app.saveCounterState(current, &batch); err != nil {
		return err
	}
	report, err := app.queue.Enqueue(batch)
	if err != nil {
		app.invalidateCounterState()
		return fmt.Errorf("queue traffic batch: %w", err)
	}
	if err := app.saveCounterState(current, nil); err != nil {
		app.invalidateCounterState()
		return err
	}
	app.previous = current
	if err := app.enqueueSpoolHealth(observedAt, report); err != nil {
		return err
	}
	return app.drain(ctx)
}

func (app *App) recoverCounterState(observedAt time.Time) error {
	if app.counterState == nil || app.stateLoaded {
		return nil
	}
	checkpoint, found, err := app.counterState.Load()
	if err != nil {
		return fmt.Errorf("load interface counter checkpoint: %w", err)
	}
	if !found || checkpoint.NodeID != app.nodeID {
		app.stateLoaded = true
		return nil
	}
	if checkpoint.PendingBatch != nil {
		report, err := app.queue.Enqueue(*checkpoint.PendingBatch)
		if err != nil {
			return fmt.Errorf("recover pending interface batch: %w", err)
		}
		if err := app.counterState.Save(interfacecounter.NewCheckpoint(app.nodeID, checkpoint.Counters, nil)); err != nil {
			return fmt.Errorf("clear recovered interface batch: %w", err)
		}
		if err := app.enqueueSpoolHealth(observedAt, report); err != nil {
			return err
		}
	}
	app.previous = checkpoint.Counters
	app.stateLoaded = true
	return nil
}

func (app *App) saveCounterState(counters map[string]interfacecounter.Counter, pending *model.Batch) error {
	if app.counterState == nil {
		return nil
	}
	if err := app.counterState.Save(interfacecounter.NewCheckpoint(app.nodeID, counters, pending)); err != nil {
		return fmt.Errorf("save interface counter checkpoint: %w", err)
	}
	return nil
}

func (app *App) invalidateCounterState() {
	app.previous = nil
	app.stateLoaded = false
}

func (app *App) Record(ctx context.Context, observedAt time.Time, events []model.Event) error {
	if len(events) == 0 {
		return app.drain(ctx)
	}
	for offset, part := 0, 0; offset < len(events); offset, part = offset+5000, part+1 {
		end := offset + 5000
		if end > len(events) {
			end = len(events)
		}
		batch := model.Batch{
			SchemaVersion: 1,
			BatchID:       detailBatchID(app.nodeID, observedAt, part, events[offset:end]),
			NodeID:        app.nodeID,
			SentAt:        observedAt,
			Events:        append([]model.Event(nil), events[offset:end]...),
		}
		report, err := app.queue.Enqueue(batch)
		if err != nil {
			return fmt.Errorf("queue detailed traffic batch: %w", err)
		}
		if err := app.enqueueSpoolHealth(observedAt, report); err != nil {
			return err
		}
	}
	return app.drain(ctx)
}

func detailBatchID(nodeID string, observedAt time.Time, part int, events []model.Event) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "%s\x00%d\x00%d", nodeID, observedAt.UnixNano(), part)
	for _, event := range events {
		_, _ = fmt.Fprintf(digest, "\x00%s", event.ID)
	}
	return fmt.Sprintf("detail-batch-%d:%x", part, digest.Sum(nil)[:12])
}

func (app *App) enqueueSpoolHealth(observedAt time.Time, report spool.DropReport) error {
	events := make([]model.Event, 0, 2)
	appendHealth := func(status, code, message string, dropped int64, usage float64) {
		events = append(events, model.Event{
			ID: eventID("health-event-"+code, app.nodeID, observedAt), ObservedAt: observedAt, Kind: model.EventHealth,
			Health: &model.HealthEvent{Collector: "spool", Status: status, Code: code, DroppedEvents: dropped, UsagePercent: usage, Message: message},
		})
	}
	if report.DroppedBatches > 0 {
		usage := report.UsagePercent
		if usage < 100 {
			usage = 100
		}
		appendHealth("degraded", report.Code, fmt.Sprintf("evicted %d queued batches", report.DroppedBatches), report.DroppedEvents, usage)
	}
	pressured := report.UsagePercent >= 80
	if pressured && !app.spoolPressured {
		appendHealth("degraded", "buffer_pressure", fmt.Sprintf("spool usage reached %.1f percent", report.UsagePercent), 0, report.UsagePercent)
	} else if !pressured && app.spoolPressured {
		appendHealth("healthy", "buffer_recovered", "spool usage returned below 80 percent", 0, report.UsagePercent)
	}
	app.spoolPressured = pressured
	if len(events) == 0 {
		return nil
	}
	health := model.Batch{
		SchemaVersion: 1,
		BatchID:       eventID("health-batch", app.nodeID, observedAt),
		NodeID:        app.nodeID,
		SentAt:        observedAt,
		Events:        events,
	}
	if _, err := app.queue.Enqueue(health); err != nil {
		return fmt.Errorf("queue spool health batch: %w", err)
	}
	return nil
}

func (app *App) drain(ctx context.Context) error {
	for {
		item, ok, err := app.queue.Peek()
		if err != nil {
			return fmt.Errorf("read queued batch: %w", err)
		}
		if !ok {
			return nil
		}
		if err := app.sender.Send(ctx, item.Batch); err != nil {
			return fmt.Errorf("deliver queued batch: %w", err)
		}
		if err := app.queue.Ack(item); err != nil {
			return fmt.Errorf("acknowledge queued batch: %w", err)
		}
	}
}

func eventID(kind, nodeID string, at time.Time) string {
	digest := sha256.Sum256([]byte(nodeID))
	return fmt.Sprintf("%s:%x:%d", kind, digest[:8], at.UnixNano())
}
