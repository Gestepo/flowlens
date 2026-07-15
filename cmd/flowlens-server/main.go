package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flowlens/internal/server/alerts"
	"flowlens/internal/server/auth"
	serverconfig "flowlens/internal/server/config"
	"flowlens/internal/server/geoip"
	"flowlens/internal/server/httpapi"
	"flowlens/internal/server/ingest"
	"flowlens/internal/server/operations"
	"flowlens/internal/server/overview"
	"flowlens/internal/server/partition"
	"flowlens/internal/server/retention"
	"flowlens/internal/server/rollup"
	"flowlens/internal/server/scheduler"
	"flowlens/internal/server/store"
	"flowlens/internal/server/trafficquery"
	"flowlens/internal/server/webhook"
	"flowlens/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := serverconfig.FromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if err := partition.NewService(pool).Ensure(ctx, time.Now().UTC()); err != nil {
		return fmt.Errorf("ensure detail partitions: %w", err)
	}
	geoResolver := geoip.New()
	defer geoResolver.Close()
	if err := geoResolver.Reload(config.GeoIPCountryPath, config.GeoIPASNPath); err != nil {
		logger.Warn("GeoIP databases unavailable; public network enrichment will be unknown", "error", err)
	}

	ingestion := ingest.NewHandler(config.AgentToken, store.New(pool, store.WithGeoIP(geoResolver)))
	overviewHandler := overview.NewHandler(overview.NewService(pool, time.Now))
	geoReloadHandler := geoip.NewReloadHandler(config.AgentToken, config.GeoIPCountryPath, config.GeoIPASNPath, geoResolver)
	trafficHandler := trafficquery.NewHandler(trafficquery.NewService(pool), time.Now)
	authHandler := auth.NewHandler(auth.NewPostgresStore(pool), config.BootstrapToken, time.Now)
	webhookRepository := webhook.NewRepository(pool)
	webhookHandler, err := webhook.NewSettingsHandler(webhookRepository, config.SecretKey, nil, config.WebhookAllowHTTP)
	if err != nil {
		return fmt.Errorf("configure webhook settings: %w", err)
	}
	webhookDispatcher, err := webhook.NewDispatcher(webhookRepository, config.SecretKey, nil, config.WebhookAllowHTTP, config.PublicURL, time.Now)
	if err != nil {
		return fmt.Errorf("configure webhook dispatcher: %w", err)
	}
	go runWebhookDispatcher(ctx, logger, webhookDispatcher)
	go runOperationsScheduler(ctx, logger, pool, config.DatabaseBudgetBytes)
	operationsHandler := operations.NewHandler(operations.NewPostgresService(pool, config.DatabaseBudgetBytes))
	server := &http.Server{
		Addr: config.ListenAddress,
		Handler: httpapi.NewAppRouter(ingestion, config.WebDir,
			httpapi.WithOverview(overviewHandler),
			httpapi.WithBrowserAuth(
				http.HandlerFunc(authHandler.Bootstrap), http.HandlerFunc(authHandler.Login),
				http.HandlerFunc(authHandler.Logout), http.HandlerFunc(authHandler.Password),
				http.HandlerFunc(authHandler.Session), authHandler.RequireSession,
			),
			httpapi.WithGeoIPReload(geoReloadHandler),
			httpapi.WithTrafficQueries(
				http.HandlerFunc(trafficHandler.Live), http.HandlerFunc(trafficHandler.Domains), http.HandlerFunc(trafficHandler.Owners),
				http.HandlerFunc(trafficHandler.Owner), http.HandlerFunc(trafficHandler.Flows),
			),
			httpapi.WithWebhookSettings(http.HandlerFunc(webhookHandler.Get), http.HandlerFunc(webhookHandler.Put), http.HandlerFunc(webhookHandler.Test)),
			httpapi.WithOperations(
				http.HandlerFunc(operationsHandler.Health), http.HandlerFunc(operationsHandler.Alerts), http.HandlerFunc(operationsHandler.Alert),
				http.HandlerFunc(operationsHandler.Settings), http.HandlerFunc(operationsHandler.Nodes), http.HandlerFunc(operationsHandler.Retention),
				http.HandlerFunc(operationsHandler.AlertSettings), http.HandlerFunc(operationsHandler.Node),
			),
		),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	logger.Info("server listening", "address", config.ListenAddress)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		return nil
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}

func runWebhookDispatcher(ctx context.Context, logger *slog.Logger, dispatcher *webhook.Dispatcher) {
	hostname, _ := os.Hostname()
	owner := fmt.Sprintf("%s-%d", hostname, os.Getpid())
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		worked, err := dispatcher.RunOne(ctx, owner)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("webhook delivery failed", "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runOperationsScheduler(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, databaseBudgetBytes int64) {
	hostname, _ := os.Hostname()
	owner := fmt.Sprintf("%s-%d-operations", hostname, os.Getpid())
	service := scheduler.New(scheduler.NewPostgresStore(pool), owner, time.Now)
	rollups := rollup.NewService(pool)
	cleanup := retention.NewService(pool)
	partitions := partition.NewService(pool)
	alertRunner := alerts.NewRunner(alerts.NewPostgresSource(pool, alerts.WithDatabaseBudgetBytes(databaseBudgetBytes)), alerts.NewRepository(pool))
	jobs := []scheduler.Job{
		{Name: "alert-evaluation", Interval: time.Minute, Run: func(jobCtx context.Context) error {
			return alertRunner.Run(jobCtx, time.Now().UTC())
		}},
		{Name: "hour-rollup", Interval: time.Hour, NextRun: nextHourRollup, Run: func(jobCtx context.Context) error {
			end := time.Now().UTC().Truncate(time.Hour)
			return rollups.RollupRange(jobCtx, rollup.ResolutionHour, end.Add(-2*time.Hour), end)
		}},
		{Name: "day-rollup", Interval: 24 * time.Hour, NextRun: nextDayRollup, Run: func(jobCtx context.Context) error {
			end := time.Now().UTC().Truncate(24 * time.Hour)
			return rollups.RollupRange(jobCtx, rollup.ResolutionDay, end.Add(-48*time.Hour), end)
		}},
		{Name: "retention", Interval: time.Hour, Run: func(jobCtx context.Context) error {
			var settings retention.Settings
			if err := pool.QueryRow(jobCtx, `SELECT detail_retention_days,aggregate_retention_months FROM operation_settings WHERE id=1`).Scan(&settings.DetailDays, &settings.AggregateMonths); err != nil {
				return err
			}
			_, err := cleanup.Cleanup(jobCtx, time.Now().UTC(), settings)
			return err
		}},
		{Name: "partition-maintenance", Interval: 24 * time.Hour, Run: func(jobCtx context.Context) error {
			return partitions.Ensure(jobCtx, time.Now().UTC())
		}},
	}
	if err := service.Run(ctx, jobs); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("operations scheduler stopped", "error", err)
	}
}

func nextHourRollup(at time.Time) time.Time {
	next := at.UTC().Truncate(time.Hour).Add(10 * time.Minute)
	if !next.After(at) {
		next = next.Add(time.Hour)
	}
	return next
}

func nextDayRollup(at time.Time) time.Time {
	next := at.UTC().Truncate(24 * time.Hour).Add(20 * time.Minute)
	if !next.After(at) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
