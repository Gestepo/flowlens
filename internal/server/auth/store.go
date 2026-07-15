package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (store *PostgresStore) AdminCount(ctx context.Context) (int, error) {
	var count int
	err := store.pool.QueryRow(ctx, "SELECT count(*) FROM administrators").Scan(&count)
	return count, err
}

func (store *PostgresStore) CreateAdmin(ctx context.Context, username, passwordHash string) (Admin, error) {
	admin := Admin{}
	err := store.pool.QueryRow(ctx, `
		INSERT INTO administrators (id, username, password_hash) VALUES (1, $1, $2)
		RETURNING id, username, password_hash
	`, username, passwordHash).Scan(&admin.ID, &admin.Username, &admin.PasswordHash)
	return admin, err
}

func (store *PostgresStore) FindAdmin(ctx context.Context, username string) (Admin, error) {
	admin := Admin{}
	err := store.pool.QueryRow(ctx, `SELECT id, username, password_hash FROM administrators WHERE username=$1`, username).
		Scan(&admin.ID, &admin.Username, &admin.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	return admin, err
}

func (store *PostgresStore) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	result, err := store.pool.Exec(ctx, `UPDATE administrators SET password_hash=$2, updated_at=now() WHERE id=$1`, id, passwordHash)
	if err == nil && result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (store *PostgresStore) CreateSession(ctx context.Context, session Session) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO browser_sessions
		(token_hash, administrator_id, csrf_token, created_at, last_seen_at, idle_expires_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, session.TokenHash[:], session.AdminID, session.CSRFToken, session.CreatedAt, session.LastSeenAt, session.IdleExpiresAt, session.ExpiresAt)
	return err
}

func (store *PostgresStore) FindSession(ctx context.Context, hash [32]byte) (Session, error) {
	session := Session{}
	var storedHash []byte
	err := store.pool.QueryRow(ctx, `
		SELECT s.token_hash, s.administrator_id, a.username, s.csrf_token, s.created_at,
		       s.last_seen_at, s.idle_expires_at, s.expires_at
		FROM browser_sessions s JOIN administrators a ON a.id=s.administrator_id
		WHERE s.token_hash=$1
	`, hash[:]).Scan(&storedHash, &session.AdminID, &session.Username, &session.CSRFToken, &session.CreatedAt,
		&session.LastSeenAt, &session.IdleExpiresAt, &session.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err == nil {
		copy(session.TokenHash[:], storedHash)
	}
	return session, err
}

func (store *PostgresStore) TouchSession(ctx context.Context, hash [32]byte, lastSeen, idleExpires time.Time) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE browser_sessions SET last_seen_at=$2, idle_expires_at=LEAST($3, expires_at) WHERE token_hash=$1
	`, hash[:], lastSeen, idleExpires)
	if err == nil && result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (store *PostgresStore) DeleteSession(ctx context.Context, hash [32]byte) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM browser_sessions WHERE token_hash=$1`, hash[:])
	return err
}

func (store *PostgresStore) DeleteAdminSessions(ctx context.Context, adminID int64) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM browser_sessions WHERE administrator_id=$1`, adminID)
	return err
}
