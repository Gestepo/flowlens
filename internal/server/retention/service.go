package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Settings struct {
	DetailDays      int
	AggregateMonths int
}

type Stats struct {
	StartedAt   time.Time
	FinishedAt  time.Time
	DeletedRows int64
}

type Service struct {
	pool  *pgxpool.Pool
	pause func(context.Context) error
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, pause: func(ctx context.Context) error {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
}

func (service *Service) Cleanup(ctx context.Context, now time.Time, settings Settings) (Stats, error) {
	stats := Stats{StartedAt: now}
	if settings.DetailDays < 1 || settings.DetailDays > 30 || settings.AggregateMonths < 1 || settings.AggregateMonths > 12 {
		return stats, errors.New("retention settings are outside supported bounds")
	}
	detailCutoff := now.UTC().AddDate(0, 0, -settings.DetailDays)
	dayCutoff := now.UTC().AddDate(0, -settings.AggregateMonths, 0)
	detailTables := []struct{ table, column string }{
		{"connection_details", "observed_at"}, {"proxy_request_details", "observed_at"},
		{"interface_deltas", "observed_at"}, {"domain_evidence", "observed_at"},
	}
	for _, table := range detailTables {
		condition := fmt.Sprintf(`t.%s < $1 AND EXISTS (
			SELECT 1 FROM traffic_rollups r WHERE r.resolution='day' AND r.node_id=t.node_id AND r.bucket=date_trunc('day',t.%s,'UTC')
		)`, table.column, table.column)
		deleted, err := service.deleteBatched(ctx, table.table, condition, detailCutoff)
		stats.DeletedRows += deleted
		if err != nil {
			return stats, err
		}
	}
	minuteTables := []string{"traffic_minute", "owner_minute", "domain_minute", "flow_minute", "proxy_status_minute"}
	for _, table := range minuteTables {
		condition := `t.bucket < $1 AND EXISTS (
			SELECT 1 FROM traffic_rollups r WHERE r.resolution='day' AND r.node_id=t.node_id AND r.bucket=date_trunc('day',t.bucket,'UTC')
		)`
		deleted, err := service.deleteBatched(ctx, table, condition, detailCutoff)
		stats.DeletedRows += deleted
		if err != nil {
			return stats, err
		}
	}
	for _, cleanup := range []struct {
		table, condition string
		cutoff           time.Time
	}{
		{"traffic_rollups", "t.resolution='hour' AND t.bucket < $1 AND EXISTS (SELECT 1 FROM traffic_rollups d WHERE d.resolution='day' AND d.node_id=t.node_id AND d.bucket=date_trunc('day',t.bucket,'UTC'))", detailCutoff},
		{"traffic_rollups", "t.resolution='day' AND t.bucket < $1", dayCutoff},
		{"alerts", "t.status='resolved' AND t.last_seen_at < $1", dayCutoff},
		{"webhook_deliveries", "t.created_at < $1 AND t.status IN ('delivered','terminal','cancelled')", dayCutoff},
		{"collector_health", "t.observed_at < $1", detailCutoff},
	} {
		deleted, err := service.deleteBatched(ctx, cleanup.table, cleanup.condition, cleanup.cutoff)
		stats.DeletedRows += deleted
		if err != nil {
			return stats, err
		}
	}
	stats.FinishedAt = time.Now().UTC()
	return stats, nil
}

func (service *Service) deleteBatched(ctx context.Context, table, condition string, cutoff time.Time) (int64, error) {
	query := fmt.Sprintf(`DELETE FROM %s t WHERE (t.tableoid,t.ctid) IN (
		SELECT candidate.tableoid,candidate.ctid FROM %s candidate
		WHERE %s LIMIT 10000
	)`, table, table, strings.ReplaceAll(condition, "t.", "candidate."))
	var total int64
	for {
		result, err := service.pool.Exec(ctx, query, cutoff)
		if err != nil {
			return total, fmt.Errorf("clean %s: %w", table, err)
		}
		deleted := result.RowsAffected()
		total += deleted
		if deleted < 10000 {
			return total, nil
		}
		if err := service.pause(ctx); err != nil {
			return total, err
		}
	}
}
