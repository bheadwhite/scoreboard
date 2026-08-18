// Package store is the persistence layer: one file per aggregate, all of it
// behind a single *Store handed to the services.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup finds nothing. Services map it to
// connect.CodeNotFound.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write loses a race or violates an invariant
// that is the caller's fault. Services map it to connect.CodeFailedPrecondition.
var ErrConflict = errors.New("conflict")

type Store struct {
	pool *pgxpool.Pool
}

// New opens the pool against databaseURL.
//
// The settings here are what make this safe behind a transaction pooler
// (Supabase port 6543, PgBouncer in transaction mode), which is how a Cloud Run
// service with many short-lived instances has to connect:
//
//   - Prepared statements are disabled. A transaction pooler hands out a
//     different backend connection per transaction, so a named prepared
//     statement created on one is missing on the next.
//   - MaxConns is deliberately small. Instances multiply under load and the
//     free-tier connection ceiling is shared across all of them.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 2 * time.Minute
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Pool exposes the underlying pool for migrations and health checks.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) Close() { s.pool.Close() }

// Ping verifies the database is reachable. Also serves as the keepalive that
// stops a free-tier project from pausing for inactivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Tx runs fn inside a transaction, rolling back on error. Exported for the
// maintenance commands in cmd/, which need multi-statement atomicity without a
// dedicated store method for each one-off.
func (s *Store) Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return s.tx(ctx, fn)
}

// tx runs fn inside a transaction, rolling back on error.
func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	t, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = t.Rollback(ctx) }()

	if err := fn(t); err != nil {
		return err
	}
	return t.Commit(ctx)
}

// noRows maps pgx's sentinel onto ours so services only know about ErrNotFound.
func noRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// placeholderProvider marks a users row nobody can sign in to. A placeholder is
// an ordinary player that another player added so a game they were actually in
// could be recorded honestly; what they lack is a provider identity, so that is
// what says so.
//
// Never 'google', so the sign-in upsert — which conflicts on
// (provider, provider_id) — can never land on one of these rows.
const placeholderProvider = "placeholder"

// isPlaceholder is the SQL half of model.User.Placeholder. Written once and
// referenced from every query that loads a player, because a screen that
// disagrees with the concession protocol about who can answer is worse than
// either answer.
const isPlaceholder = `(u.provider = '` + placeholderProvider + `')`
