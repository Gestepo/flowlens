package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	serverhealth "flowlens/internal/server/health"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultDatabaseBudgetBytes = int64(10 << 30)

type PostgresService struct {
	pool                *pgxpool.Pool
	databaseBudgetBytes int64
}

func NewPostgresService(pool *pgxpool.Pool, budget ...int64) *PostgresService {
	configured := defaultDatabaseBudgetBytes
	if len(budget) > 0 && budget[0] > 0 {
		configured = budget[0]
	}
	return &PostgresService{pool: pool, databaseBudgetBytes: configured}
}

func (service *PostgresService) ListNodes(ctx context.Context, now time.Time) ([]Node, error) {
	rows, err := service.pool.Query(ctx, `SELECT id,name,coalesce(last_seen_at,'epoch') FROM nodes ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []Node
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.ID, &node.Name, &node.LastSeenAt); err != nil {
			return nil, err
		}
		failed, err := service.failedCollectors(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		state := serverhealth.Classify(now, serverhealth.Snapshot{LastSeenAt: node.LastSeenAt, FailedCollectors: failed})
		node.Status, node.FailedCollectors = state.Status, failed
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (service *PostgresService) failedCollectors(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := service.pool.Query(ctx, `
		SELECT collector FROM (
			SELECT DISTINCT ON (collector) collector,status FROM collector_health WHERE node_id=$1 ORDER BY collector,observed_at DESC
		) latest WHERE status <> 'healthy' ORDER BY collector
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (service *PostgresService) Health(ctx context.Context, now time.Time) (HealthResponse, error) {
	nodes, err := service.ListNodes(ctx, now)
	if err != nil {
		return HealthResponse{}, err
	}
	status := serverhealth.StatusHealthy
	priority := map[string]int{serverhealth.StatusHealthy: 0, serverhealth.StatusWarning: 1, serverhealth.StatusDelayed: 2, serverhealth.StatusPartial: 3, serverhealth.StatusOffline: 4}
	for _, node := range nodes {
		if priority[node.Status] > priority[status] {
			status = node.Status
		}
	}
	var databaseUsage float64
	if err := service.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())::float8 / $1 * 100`, service.databaseBudgetBytes).Scan(&databaseUsage); err != nil {
		return HealthResponse{}, err
	}
	if databaseUsage > 80 && priority[serverhealth.StatusWarning] > priority[status] {
		status = serverhealth.StatusWarning
	}
	var terminal int
	if err := service.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE status='terminal'`).Scan(&terminal); err != nil {
		return HealthResponse{}, err
	}
	jobs := make(map[string]string)
	rows, err := service.pool.Query(ctx, `SELECT name,last_error,last_success_at FROM job_leases ORDER BY name`)
	if err != nil {
		return HealthResponse{}, err
	}
	for rows.Next() {
		var name, lastError string
		var success *time.Time
		if err := rows.Scan(&name, &lastError, &success); err != nil {
			rows.Close()
			return HealthResponse{}, err
		}
		if lastError != "" {
			jobs[name] = "failed"
		} else if success == nil {
			jobs[name] = "pending"
		} else {
			jobs[name] = "healthy"
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return HealthResponse{}, err
	}
	rows.Close()
	return HealthResponse{Status: status, Nodes: nodes, DatabaseUsagePercent: databaseUsage, WebhookTerminalFailures: terminal, Jobs: jobs}, nil
}

func (service *PostgresService) ListAlerts(ctx context.Context, status string, limit, offset int) ([]Alert, error) {
	rows, err := service.pool.Query(ctx, `
		SELECT id,rule_id,status,severity,node_id,owner_id,title,evidence,observed_value,comparison_value,window_seconds,first_seen_at,last_seen_at,resolved_at,occurrence_count
		FROM alerts WHERE ($1='all' OR status=$1) ORDER BY last_seen_at DESC,id DESC LIMIT $2 OFFSET $3
	`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var alerts []Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func (service *PostgresService) GetAlert(ctx context.Context, id int64) (Alert, error) {
	row := service.pool.QueryRow(ctx, `
		SELECT id,rule_id,status,severity,node_id,owner_id,title,evidence,observed_value,comparison_value,window_seconds,first_seen_at,last_seen_at,resolved_at,occurrence_count
		FROM alerts WHERE id=$1
	`, id)
	alert, err := scanAlert(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	if err != nil {
		return Alert{}, err
	}
	rows, err := service.pool.Query(ctx, `SELECT id,status,attempt,response_status,last_error,created_at,delivered_at FROM webhook_deliveries WHERE alert_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return Alert{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var delivery Delivery
		if err := rows.Scan(&delivery.ID, &delivery.Status, &delivery.Attempt, &delivery.ResponseStatus, &delivery.LastError, &delivery.CreatedAt, &delivery.DeliveredAt); err != nil {
			return Alert{}, err
		}
		alert.Deliveries = append(alert.Deliveries, delivery)
	}
	return alert, rows.Err()
}

type alertScanner interface{ Scan(...any) error }

func scanAlert(row alertScanner) (Alert, error) {
	var alert Alert
	var evidence []byte
	err := row.Scan(&alert.ID, &alert.RuleID, &alert.Status, &alert.Severity, &alert.NodeID, &alert.OwnerID, &alert.Title, &evidence, &alert.ObservedValue, &alert.ComparisonValue, &alert.WindowSeconds, &alert.FirstSeenAt, &alert.LastSeenAt, &alert.ResolvedAt, &alert.OccurrenceCount)
	if err != nil {
		return Alert{}, err
	}
	if err := json.Unmarshal(evidence, &alert.Evidence); err != nil {
		return Alert{}, err
	}
	alert.TrafficFilter = TrafficFilter(alert.Evidence)
	return alert, nil
}

func (service *PostgresService) GetSettings(ctx context.Context) (Settings, error) {
	settings := Settings{}
	if err := service.pool.QueryRow(ctx, `SELECT detail_retention_days,aggregate_retention_months FROM operation_settings WHERE id=1`).Scan(&settings.DetailRetentionDays, &settings.AggregateRetentionMonths); err != nil {
		return settings, err
	}
	rows, err := service.pool.Query(ctx, `SELECT id,name,enabled,severity,config FROM alert_rules ORDER BY id`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule AlertRule
		var encoded []byte
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.Severity, &encoded); err != nil {
			return settings, err
		}
		var config map[string]float64
		if err := json.Unmarshal(encoded, &config); err != nil {
			return settings, err
		}
		rule.Threshold, rule.Multiplier = config["threshold"], config["multiplier"]
		settings.AlertRules = append(settings.AlertRules, rule)
	}
	return settings, rows.Err()
}

func (service *PostgresService) UpdateRetention(ctx context.Context, detail, months int) error {
	_, err := service.pool.Exec(ctx, `UPDATE operation_settings SET detail_retention_days=$1,aggregate_retention_months=$2,updated_at=now() WHERE id=1`, detail, months)
	return err
}

func (service *PostgresService) UpdateAlertRules(ctx context.Context, rules []AlertRule) error {
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, rule := range rules {
		config, err := json.Marshal(map[string]float64{"threshold": rule.Threshold, "multiplier": rule.Multiplier})
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE alert_rules SET enabled=$2,severity=$3,config=config||$4::jsonb,updated_at=now() WHERE id=$1`, rule.ID, rule.Enabled, rule.Severity, config)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("%w: alert rule", ErrNotFound)
		}
	}
	return tx.Commit(ctx)
}

func (service *PostgresService) RenameNode(ctx context.Context, id, name string) error {
	result, err := service.pool.Exec(ctx, `UPDATE nodes SET name=$2 WHERE id=$1`, id, name)
	if err == nil && result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
