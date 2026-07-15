package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrLeaseLost = errors.New("webhook delivery lease is no longer owned by this worker")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) Claim(ctx context.Context, owner string, now time.Time) (Delivery, bool, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Delivery{}, false, err
	}
	defer tx.Rollback(ctx)
	delivery := Delivery{}
	var alertID *int64
	var severity, title *string
	var evidence, testPayload []byte
	var occurredAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT d.id,d.alert_id,d.event_type,d.attempt,a.severity,a.title,a.evidence,a.first_seen_at,d.payload
		FROM webhook_deliveries d LEFT JOIN alerts a ON a.id=d.alert_id
		WHERE (d.status='pending' AND d.next_attempt_at <= $1)
		   OR (d.status='leased' AND d.lease_expires_at < $1)
		ORDER BY d.next_attempt_at,d.id
		FOR UPDATE OF d SKIP LOCKED LIMIT 1
	`, now).Scan(&delivery.ID, &alertID, &delivery.EventType, &delivery.Attempt, &severity,
		&title, &evidence, &occurredAt, &testPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, fmt.Errorf("claim webhook delivery: %w", err)
	}
	if alertID != nil {
		delivery.AlertID = *alertID
		delivery.Severity, delivery.Title, delivery.OccurredAt = *severity, *title, *occurredAt
		if err := json.Unmarshal(evidence, &delivery.Evidence); err != nil {
			return Delivery{}, false, fmt.Errorf("decode webhook evidence: %w", err)
		}
	} else if err := json.Unmarshal(testPayload, &delivery); err != nil {
		return Delivery{}, false, fmt.Errorf("decode test webhook payload: %w", err)
	}
	delivery.PublicID = fmt.Sprintf("delivery-%d", delivery.ID)
	if _, err := tx.Exec(ctx, `
		UPDATE webhook_deliveries SET status='leased',lease_owner=$2,lease_expires_at=$3 WHERE id=$1
	`, delivery.ID, owner, now.Add(2*time.Minute)); err != nil {
		return Delivery{}, false, fmt.Errorf("lease webhook delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Delivery{}, false, err
	}
	return delivery, true, nil
}

func (repository *Repository) GetSettings(ctx context.Context) (WebhookSettings, error) {
	settings := WebhookSettings{}
	err := repository.pool.QueryRow(ctx, `
		SELECT enabled,endpoint,encrypted_secret,updated_at FROM webhook_settings WHERE id=1
	`).Scan(&settings.Enabled, &settings.Endpoint, &settings.EncryptedSecret, &settings.UpdatedAt)
	return settings, err
}

func (repository *Repository) SaveSettings(ctx context.Context, settings WebhookSettings) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE webhook_settings SET enabled=$1,endpoint=$2,encrypted_secret=$3,updated_at=$4 WHERE id=1
	`, settings.Enabled, settings.Endpoint, settings.EncryptedSecret, settings.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE webhook_deliveries SET status='cancelled',lease_owner=NULL,lease_expires_at=NULL
		WHERE event_type='test' AND status IN ('pending','leased')
	`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *Repository) QueueTest(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	payload, err := json.Marshal(Delivery{
		EventType: "test", Severity: "info", Title: "Webhook 测试", Evidence: map[string]string{"source": "settings"}, OccurredAt: now,
	})
	if err != nil {
		return 0, err
	}
	var id int64
	err = repository.pool.QueryRow(ctx, `
		INSERT INTO webhook_deliveries(event_type,payload,status,next_attempt_at,created_at)
		VALUES ('test',$1,'pending',$2,$2) RETURNING id
	`, payload, now).Scan(&id)
	return id, err
}

func (repository *Repository) Complete(ctx context.Context, id int64, owner string, status int, excerpt string, now time.Time) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status='delivered',response_status=$3,response_excerpt=$4,last_error='',
		delivered_at=$5,lease_owner=NULL,lease_expires_at=NULL
		WHERE id=$1 AND status='leased' AND lease_owner=$2
	`, id, owner, status, cleanExcerpt(excerpt), now)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (repository *Repository) Fail(ctx context.Context, id int64, owner string, status int, message, excerpt string, next time.Time, terminal bool) error {
	result, err := repository.pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status=CASE WHEN $7 THEN 'terminal' ELSE 'pending' END,
		attempt=attempt+1,response_status=NULLIF($3,0),last_error=$4,response_excerpt=$5,
		next_attempt_at=CASE WHEN $7 THEN next_attempt_at ELSE $6 END,lease_owner=NULL,lease_expires_at=NULL
		WHERE id=$1 AND status='leased' AND lease_owner=$2
	`, id, owner, status, cleanExcerpt(message), cleanExcerpt(excerpt), next, terminal)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func cleanExcerpt(value string) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) > 64<<10 {
		value = value[:64<<10]
		value = strings.ToValidUTF8(value, "�")
	}
	return value
}
