package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) Reconcile(ctx context.Context, rule Rule, nodeID string, findings []Finding, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin alert reconciliation: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO alert_rules(id,kind,name,enabled,severity,config,updated_at)
		VALUES ($1,$2,$3,$4,$5,jsonb_build_object('threshold',$6::float8,'multiplier',$7::float8,'window_seconds',$8::int),$9)
		ON CONFLICT (id) DO UPDATE SET kind=EXCLUDED.kind,name=EXCLUDED.name,enabled=EXCLUDED.enabled,
		severity=EXCLUDED.severity,config=EXCLUDED.config,updated_at=EXCLUDED.updated_at
	`, rule.ID, rule.Kind, rule.Name, rule.Enabled, rule.Severity, rule.Threshold, rule.Multiplier, rule.WindowSeconds, now); err != nil {
		return fmt.Errorf("upsert alert rule: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, Fingerprint(rule.ID, nodeID)); err != nil {
		return fmt.Errorf("lock alert rule: %w", err)
	}

	seen := make(map[string]bool, len(findings))
	for _, finding := range findings {
		if finding.RuleID != rule.ID || finding.NodeID != nodeID {
			return errors.New("finding does not belong to the reconciled rule and node")
		}
		if err := ValidateEvidence(finding.Evidence); err != nil {
			return err
		}
		seen[finding.Fingerprint] = true
		if err := repository.applyFinding(ctx, tx, finding, now); err != nil {
			return err
		}
	}
	if err := repository.applyMissing(ctx, tx, rule.ID, nodeID, seen, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit alert reconciliation: %w", err)
	}
	return nil
}

func (repository *Repository) applyFinding(ctx context.Context, tx pgx.Tx, finding Finding, now time.Time) error {
	var id int64
	current := AlertState{}
	err := tx.QueryRow(ctx, `
		SELECT id,status,severity,occurrence_count,missing_windows,first_seen_at,last_seen_at,resolved_at
		FROM alerts WHERE fingerprint=$1 AND status='open' FOR UPDATE
	`, finding.Fingerprint).Scan(&id, &current.Status, &current.Severity, &current.OccurrenceCount, &current.MissingWindows,
		&current.FirstSeenAt, &current.LastSeenAt, &current.ResolvedAt)
	evidence, marshalErr := json.Marshal(finding.Evidence)
	if marshalErr != nil {
		return fmt.Errorf("encode alert evidence: %w", marshalErr)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		row := tx.QueryRow(ctx, `
			INSERT INTO alerts(rule_id,fingerprint,severity,status,node_id,owner_id,title,evidence,observed_value,
			comparison_value,window_seconds,first_seen_at,last_seen_at)
			VALUES ($1,$2,$3,'open',$4,$5,$6,$7,$8,$9,$10,$11,$11) RETURNING id
		`, finding.RuleID, finding.Fingerprint, finding.Severity, finding.NodeID, finding.OwnerID, finding.Title, evidence,
			finding.ObservedValue, finding.ComparisonValue, finding.WindowSeconds, now)
		if err := row.Scan(&id); err != nil {
			return fmt.Errorf("open alert: %w", err)
		}
		return insertDelivery(ctx, tx, id, "opened", now)
	}
	if err != nil {
		return fmt.Errorf("load open alert: %w", err)
	}
	next, notify := ApplyFinding(&current, finding, now)
	if _, err := tx.Exec(ctx, `
		UPDATE alerts SET severity=$2,title=$3,evidence=$4,observed_value=$5,comparison_value=$6,
		window_seconds=$7,last_seen_at=$8,occurrence_count=$9,missing_windows=0
		WHERE id=$1
	`, id, next.Severity, finding.Title, evidence, finding.ObservedValue, finding.ComparisonValue,
		finding.WindowSeconds, next.LastSeenAt, next.OccurrenceCount); err != nil {
		return fmt.Errorf("update open alert: %w", err)
	}
	if notify {
		return insertDelivery(ctx, tx, id, "severity_increased", now)
	}
	return nil
}

func (repository *Repository) applyMissing(ctx context.Context, tx pgx.Tx, ruleID, nodeID string, seen map[string]bool, now time.Time) error {
	rows, err := tx.Query(ctx, `
		SELECT id,fingerprint,status,severity,occurrence_count,missing_windows,first_seen_at,last_seen_at,resolved_at
		FROM alerts WHERE rule_id=$1 AND node_id=$2 AND status='open' FOR UPDATE
	`, ruleID, nodeID)
	if err != nil {
		return fmt.Errorf("load missing alerts: %w", err)
	}
	type missingAlert struct {
		id          int64
		fingerprint string
		state       AlertState
	}
	var missing []missingAlert
	for rows.Next() {
		item := missingAlert{}
		if err := rows.Scan(&item.id, &item.fingerprint, &item.state.Status, &item.state.Severity, &item.state.OccurrenceCount,
			&item.state.MissingWindows, &item.state.FirstSeenAt, &item.state.LastSeenAt, &item.state.ResolvedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan missing alert: %w", err)
		}
		if !seen[item.fingerprint] {
			missing = append(missing, item)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate missing alerts: %w", err)
	}
	rows.Close()
	for _, item := range missing {
		next, resolved := ApplyMissing(item.state, now)
		if resolved {
			if _, err := tx.Exec(ctx, `UPDATE alerts SET status='resolved',missing_windows=$2,resolved_at=$3 WHERE id=$1`, item.id, next.MissingWindows, now); err != nil {
				return fmt.Errorf("resolve alert: %w", err)
			}
		} else if _, err := tx.Exec(ctx, `UPDATE alerts SET missing_windows=$2 WHERE id=$1`, item.id, next.MissingWindows); err != nil {
			return fmt.Errorf("mark alert missing: %w", err)
		}
	}
	return nil
}

func insertDelivery(ctx context.Context, tx pgx.Tx, alertID int64, eventType string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO webhook_deliveries(alert_id,event_type,status,next_attempt_at,created_at)
		VALUES ($1,$2,'pending',$3,$3)
	`, alertID, eventType, now)
	if err != nil {
		return fmt.Errorf("queue alert delivery: %w", err)
	}
	return nil
}
