package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (store *PostgresStore) Claim(ctx context.Context, name, owner string, now time.Time, duration time.Duration) (bool, error) {
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO job_leases(name,next_run_at) VALUES ($1,$2) ON CONFLICT (name) DO NOTHING
	`, name, now); err != nil {
		return false, err
	}
	result, err := store.pool.Exec(ctx, `
		UPDATE job_leases SET lease_owner=$2,lease_expires_at=$3
		WHERE name=$1 AND next_run_at <= $4 AND (lease_owner IS NULL OR lease_expires_at <= $4)
	`, name, owner, now.Add(duration), now)
	return err == nil && result.RowsAffected() == 1, err
}

func (store *PostgresStore) Heartbeat(ctx context.Context, name, owner string, expires time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE job_leases SET lease_expires_at=$3 WHERE name=$1 AND lease_owner=$2
	`, name, owner, expires)
	return err
}

func (store *PostgresStore) Finish(ctx context.Context, name, owner string, success bool, at, next time.Time, message string) error {
	if len(message) > 4096 {
		message = message[:4096]
	}
	_, err := store.pool.Exec(ctx, `
		UPDATE job_leases SET lease_owner=NULL,lease_expires_at=NULL,next_run_at=$4,
		last_success_at=CASE WHEN $3 THEN $5 ELSE last_success_at END,last_error=$6
		WHERE name=$1 AND lease_owner=$2
	`, name, owner, success, next, at, message)
	return err
}
