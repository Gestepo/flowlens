package alerts

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultDatabaseBudgetBytes = int64(10 << 30)

type SourceOption func(*PostgresSource)

type PostgresSource struct {
	pool                *pgxpool.Pool
	databaseBudgetBytes int64
}

func WithDatabaseBudgetBytes(value int64) SourceOption {
	return func(source *PostgresSource) {
		if value > 0 {
			source.databaseBudgetBytes = value
		}
	}
}

func NewPostgresSource(pool *pgxpool.Pool, options ...SourceOption) *PostgresSource {
	source := &PostgresSource{pool: pool, databaseBudgetBytes: defaultDatabaseBudgetBytes}
	for _, option := range options {
		option(source)
	}
	return source
}

func (source *PostgresSource) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := source.pool.Query(ctx, `SELECT id,kind,name,enabled,severity,config FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []Rule
	for rows.Next() {
		var rule Rule
		var encoded []byte
		if err := rows.Scan(&rule.ID, &rule.Kind, &rule.Name, &rule.Enabled, &rule.Severity, &encoded); err != nil {
			return nil, err
		}
		var config map[string]float64
		if err := json.Unmarshal(encoded, &config); err != nil {
			return nil, err
		}
		rule.Threshold, rule.Multiplier, rule.WindowSeconds = config["threshold"], config["multiplier"], int(config["window_seconds"])
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (source *PostgresSource) Nodes(ctx context.Context) ([]string, error) {
	rows, err := source.pool.Query(ctx, `SELECT id FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		nodes = append(nodes, id)
	}
	return nodes, rows.Err()
}

func (source *PostgresSource) Observations(ctx context.Context, rule Rule, nodeID string, now time.Time) ([]Observation, error) {
	base := Observation{NodeID: nodeID}
	windowStart := now.Add(-5 * time.Minute)
	switch rule.Kind {
	case KindAbsoluteRate:
		err := source.pool.QueryRow(ctx, `SELECT coalesce(sum(bytes),0)::float8/300 FROM traffic_minute WHERE node_id=$1 AND bucket >= $2 AND bucket < $3`, nodeID, windowStart, now).Scan(&base.RateBPS)
		return []Observation{base}, err
	case KindOwnerBaseline:
		rows, err := source.pool.Query(ctx, `WITH current_window AS (
			SELECT owner_id,sum(bytes)::float8 bytes FROM owner_minute WHERE node_id=$1 AND bucket >= $2 AND bucket < $3 GROUP BY owner_id
		), baseline AS (
			SELECT owner_id,sum(bytes)::float8/12 bytes FROM owner_minute WHERE node_id=$1 AND bucket >= $4 AND bucket < $2 GROUP BY owner_id
		) SELECT c.owner_id,c.bytes,coalesce(b.bytes,0) FROM current_window c LEFT JOIN baseline b USING(owner_id)`, nodeID, windowStart, now, windowStart.Add(-time.Hour))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var values []Observation
		for rows.Next() {
			value := base
			if err := rows.Scan(&value.OwnerID, &value.OwnerBytes, &value.OwnerBaselineBytes); err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, rows.Err()
	case KindNewDestination:
		rows, err := source.pool.Query(ctx, `SELECT DISTINCT recent.owner_id,CASE WHEN recent.domain<>'' THEN recent.domain ELSE recent.destination END
		FROM flow_minute recent WHERE recent.node_id=$1 AND recent.bucket >= $2 AND recent.bucket < $3 AND NOT EXISTS (
			SELECT 1 FROM flow_minute old WHERE old.node_id=recent.node_id AND old.owner_id=recent.owner_id AND old.destination=recent.destination AND old.domain=recent.domain AND old.bucket >= $4 AND old.bucket < $2
		)`, nodeID, windowStart, now, now.AddDate(0, 0, -30))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var values []Observation
		for rows.Next() {
			value := base
			value.DestinationIsNew = true
			if err := rows.Scan(&value.OwnerID, &value.Destination); err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, rows.Err()
	case KindDomainCoverage:
		var total, identified float64
		err := source.pool.QueryRow(ctx, `SELECT coalesce(sum(bytes),0)::float8,coalesce(sum(bytes) FILTER(WHERE domain<>'' AND confidence<>'ip_only'),0)::float8 FROM flow_minute WHERE node_id=$1 AND bucket >= $2 AND bucket < $3`, nodeID, windowStart, now).Scan(&total, &identified)
		if total == 0 {
			base.DomainCoverage = 100
		} else {
			base.DomainCoverage = identified * 100 / total
		}
		return []Observation{base}, err
	case KindAgentStale:
		var lastSeen time.Time
		err := source.pool.QueryRow(ctx, `SELECT coalesce(last_seen_at,'epoch') FROM nodes WHERE id=$1`, nodeID).Scan(&lastSeen)
		base.AgentAgeSeconds = now.Sub(lastSeen).Seconds()
		return []Observation{base}, err
	case KindCollectorUnhealthy:
		rows, err := source.pool.Query(ctx, `SELECT collector,status FROM (SELECT DISTINCT ON(collector) collector,status FROM collector_health WHERE node_id=$1 ORDER BY collector,observed_at DESC) latest WHERE status<>'healthy'`, nodeID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var values []Observation
		for rows.Next() {
			value := base
			value.CollectorHealthy = false
			if err := rows.Scan(&value.Collector, new(string)); err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, rows.Err()
	case KindBufferPressure:
		err := source.pool.QueryRow(ctx, `SELECT coalesce(max(usage_percent),0)::float8
			FROM collector_health WHERE node_id=$1 AND collector='spool'
			AND code IN ('buffer_pressure','data_gap') AND observed_at >= $2 AND observed_at < $3`, nodeID, windowStart, now).Scan(&base.BufferUsagePercent)
		return []Observation{base}, err
	case KindDatabasePressure:
		err := source.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())::float8 / $1 * 100`, source.databaseBudgetBytes).Scan(&base.DatabaseUsagePercent)
		return []Observation{base}, err
	case KindWebhookFailures:
		err := source.pool.QueryRow(ctx, `SELECT count(*)::float8 FROM webhook_deliveries WHERE status='terminal'`).Scan(&base.WebhookTerminalFailures)
		return []Observation{base}, err
	default:
		return nil, nil
	}
}
