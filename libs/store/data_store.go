package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DataStore struct {
	Queries *Queries
	pool    *pgxpool.Pool
	logger  *slog.Logger
}

func OpenDataStore(ctx context.Context, url string) (*DataStore, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to open data store: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping data store: %w", err)
	}

	return &DataStore{
		Queries: New(pool),
		pool:    pool,
	}, nil
}

func (s *DataStore) Close() {
	s.pool.Close()
}

func (s *DataStore) InTx(ctx context.Context, fn func(q *Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func(tx pgx.Tx, ctx context.Context) {
		err := tx.Rollback(ctx)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to rollback transaction", "error", err)
			return
		}
	}(tx, ctx)

	if err := fn(s.Queries); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
