package store

import (
	"context"
	"fmt"
	"net/netip"

	"flowlens/internal/model"
	"flowlens/internal/server/geoip"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool  *pgxpool.Pool
	geoIP GeoIPResolver
}

type GeoIPResolver interface {
	Lookup(netip.Addr) geoip.NetworkInfo
}

type Option func(*Store)

func WithGeoIP(resolver GeoIPResolver) Option {
	return func(store *Store) { store.geoIP = resolver }
}

func New(pool *pgxpool.Pool, options ...Option) *Store {
	store := &Store{pool: pool}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *Store) InsertBatch(ctx context.Context, batch model.Batch) (bool, error) {
	if err := batch.Validate(); err != nil {
		return false, fmt.Errorf("validate batch: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin batch transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO nodes (id, name, last_seen_at)
		VALUES ($1, $1, $2)
		ON CONFLICT (id) DO UPDATE
		SET last_seen_at = GREATEST(nodes.last_seen_at, EXCLUDED.last_seen_at)
	`, batch.NodeID, batch.SentAt); err != nil {
		return false, fmt.Errorf("upsert node: %w", err)
	}

	result, err := tx.Exec(ctx, `
		INSERT INTO ingest_batches (node_id, batch_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, batch.NodeID, batch.BatchID)
	if err != nil {
		return false, fmt.Errorf("insert batch guard: %w", err)
	}
	if result.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit duplicate batch: %w", err)
		}
		return false, nil
	}

	hasInventory := false
	for _, event := range batch.Events {
		hasInventory = hasInventory || event.Kind == model.EventOwnerInventory
	}
	if hasInventory {
		if _, err := tx.Exec(ctx, `UPDATE owners SET running=false WHERE node_id=$1 AND kind='container'`, batch.NodeID); err != nil {
			return false, fmt.Errorf("mark absent container owners stopped: %w", err)
		}
	}
	for _, kind := range []model.EventKind{model.EventOwnerInventory, model.EventNameEvidence, model.EventConnection, model.EventProxyRequest, model.EventInterfaceDelta, model.EventHealth} {
		for _, event := range batch.Events {
			if event.Kind == kind {
				if err := insertEvent(ctx, tx, batch.NodeID, event, s.geoIP); err != nil {
					return false, err
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit batch: %w", err)
	}
	return true, nil
}
