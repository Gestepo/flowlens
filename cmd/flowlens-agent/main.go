package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	agentapp "flowlens/internal/agent/app"
	agentconfig "flowlens/internal/agent/config"
	dockerinventory "flowlens/internal/agent/docker"
	flowebpf "flowlens/internal/agent/ebpf"
	"flowlens/internal/agent/interfacecounter"
	"flowlens/internal/agent/namecapture"
	npmlogs "flowlens/internal/agent/npm"
	"flowlens/internal/agent/ownership"
	processlookup "flowlens/internal/agent/process"
	"flowlens/internal/agent/sender"
	"flowlens/internal/agent/spool"
	"flowlens/internal/model"

	"github.com/moby/moby/client"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := agentconfig.FromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	queue, err := spool.New(config.SpoolDir)
	if err != nil {
		return fmt.Errorf("open spool: %w", err)
	}
	delivery, err := sender.New(config.ServerEndpoint, config.AgentToken)
	if err != nil {
		return fmt.Errorf("create sender: %w", err)
	}
	readCounters := func() (map[string]interfacecounter.Counter, error) {
		file, err := os.Open(config.InterfaceCountersPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return interfacecounter.Parse(file)
	}
	counterState := interfacecounter.NewStateFile(filepath.Join(filepath.Dir(config.SpoolDir), "interface-counters.json"))
	agent := agentapp.New(config.NodeID, readCounters, queue, delivery, agentapp.WithCounterState(counterState))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	processes := processlookup.NewLookup("/proc", time.Now)
	attributor := ownership.NewAttributor(processes)
	connections := ownership.NewCollector(attributor)
	npmTailer := npmlogs.NewTailer(filepath.Join(filepath.Dir(config.SpoolDir), "npm-cursors.json"), []string{"/data/logs", "/var/lib/docker/volumes"}, true)
	npmRecovery := npmlogs.RecoveryTracker{}
	npmFailed := false
	var inventory *dockerinventory.Inventory
	if config.DockerAttribution {
		dockerClient, err := client.New(client.FromEnv)
		if err != nil {
			return fmt.Errorf("create Docker client: %w", err)
		}
		defer dockerClient.Close()
		inventory = dockerinventory.NewInventory(dockerClient, func(pid int) (dockerinventory.Cgroup, error) {
			return dockerinventory.ResolveHostCgroup("/proc", "/sys/fs/cgroup", pid)
		})
	} else {
		logger.Info("Docker attribution disabled")
		disabledAt := time.Now().UTC()
		disabledEvent := model.Event{
			ID: disabledCollectorEventID("docker", disabledAt), ObservedAt: disabledAt, Kind: model.EventHealth,
			Health: &model.HealthEvent{Collector: "docker", Status: "healthy", Code: "collector_disabled", Message: "Docker attribution is disabled"},
		}
		if err := agent.Record(ctx, disabledAt, []model.Event{disabledEvent}); err != nil && ctx.Err() == nil {
			logger.Warn("Docker disabled state delivery failed", "error", err)
		}
	}
	var currentInventory dockerinventory.Snapshot
	lastInventoryEvent := time.Time{}
	refreshInventory := func(at time.Time) {
		if inventory == nil {
			return
		}
		snapshot, refreshErr := inventory.Refresh(ctx)
		if refreshErr != nil {
			logger.Warn("Docker inventory refresh failed", "error", refreshErr)
			return
		}
		attributor.SetSnapshot(snapshot)
		if !reflect.DeepEqual(snapshot, currentInventory) || at.Sub(lastInventoryEvent) >= time.Hour {
			if recordErr := agent.Record(ctx, at, ownership.InventoryEvents(snapshot, at)); recordErr != nil && ctx.Err() == nil {
				logger.Warn("owner inventory delivery failed", "error", recordErr)
			}
			currentInventory = snapshot
			lastInventoryEvent = at
		}
	}
	refreshInventory(time.Now().UTC())

	tracer, err := flowebpf.NewTracer()
	if err != nil {
		return fmt.Errorf("start socket tracer: %w", err)
	}
	defer tracer.Close()
	observations := make(chan flowebpf.Observation, 4096)
	tracerErrors := make(chan error, 1)
	go func() { tracerErrors <- tracer.Run(ctx, observations) }()
	nameEvidence := make(chan model.NameEvidence, 256)
	nameErrors := make(chan error, 1)
	nameCaptureActive := false
	var pendingNameEvents []model.Event
	if len(config.CaptureInterfaces) > 0 {
		nameCollector, collectorErr := namecapture.NewCollector(config.CaptureInterfaces)
		if collectorErr != nil {
			logger.Warn("name capture startup failed", "error", collectorErr)
			pendingNameEvents = append(pendingNameEvents, namecapture.CollectorHealthEvent(time.Now().UTC(), "collector_unavailable", 0))
		} else {
			defer nameCollector.Close()
			go func() { nameErrors <- nameCollector.Run(ctx, nameEvidence) }()
		}
	}

	if err := agent.Sample(ctx, time.Now().UTC()); err != nil {
		logger.Warn("initial sample failed", "error", err)
	}
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	inventoryTicker := time.NewTicker(30 * time.Second)
	defer inventoryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = agent.Record(ctx, time.Now().UTC(), connections.Drain(time.Now().UTC()))
			return nil
		case observation := <-observations:
			connections.Observe(observation)
		case evidence := <-nameEvidence:
			if !nameCaptureActive {
				logger.Info("name evidence capture active", "source", evidence.Source)
				nameCaptureActive = true
			}
			pendingNameEvents = append(pendingNameEvents, namecapture.EvidenceEvent(evidence))
		case nameErr := <-nameErrors:
			if nameErr != nil && ctx.Err() == nil {
				logger.Warn("name capture stopped", "error", nameErr)
				pendingNameEvents = append(pendingNameEvents, namecapture.CollectorHealthEvent(time.Now().UTC(), "collector_unavailable", 0))
			}
			nameErrors = nil
		case tracerErr := <-tracerErrors:
			if tracerErr != nil {
				return fmt.Errorf("socket tracer stopped: %w", tracerErr)
			}
			if ctx.Err() == nil {
				return fmt.Errorf("socket tracer stopped unexpectedly")
			}
			return nil
		case observedAt := <-ticker.C:
			if err := agent.Sample(ctx, observedAt.UTC()); err != nil && ctx.Err() == nil {
				logger.Warn("traffic sample failed", "error", err)
			}
			if err := agent.Record(ctx, observedAt.UTC(), connections.Drain(observedAt.UTC())); err != nil && ctx.Err() == nil {
				logger.Warn("detailed traffic delivery failed", "error", err)
			}
			if len(pendingNameEvents) > 0 {
				if err := agent.Record(ctx, observedAt.UTC(), pendingNameEvents); err != nil && ctx.Err() == nil {
					logger.Warn("name evidence delivery failed", "error", err)
				}
				pendingNameEvents = nil
			}
			if len(config.NPMLogGlobs) > 0 {
				result, pollErr := npmTailer.Poll(config.NPMLogGlobs)
				recoveryEvent := npmRecovery.Observe(result, pollErr, observedAt.UTC())
				if pollErr != nil {
					if !npmFailed {
						if recordErr := agent.Record(ctx, observedAt.UTC(), []model.Event{npmlogs.CollectorUnavailableEvent(observedAt.UTC())}); recordErr != nil && ctx.Err() == nil {
							logger.Warn("NPM collector health delivery failed", "error", recordErr)
						}
					}
					npmFailed = true
					logger.Warn("NPM log collection failed", "error", pollErr)
				} else {
					npmFailed = false
					events := npmlogs.Events(result, observedAt.UTC())
					if recoveryEvent != nil {
						events = append(events, *recoveryEvent)
					}
					if err := agent.Record(ctx, observedAt.UTC(), events); err != nil && ctx.Err() == nil {
						logger.Warn("NPM request delivery failed", "error", err)
					}
				}
			}
		case observedAt := <-inventoryTicker.C:
			refreshInventory(observedAt.UTC())
		}
	}
}

func disabledCollectorEventID(collector string, at time.Time) string {
	return fmt.Sprintf("collector-disabled:%s:%d", collector, at.UnixNano())
}
