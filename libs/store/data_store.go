package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DataStore struct {
	Queries *Queries
	pool    *pgxpool.Pool
	logger  *slog.Logger
}

func OpenDataStore(ctx context.Context, logger *slog.Logger, url string) (*DataStore, error) {
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
		logger:  logger,
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

	if err := fn(s.Queries.WithTx(tx)); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil {
			s.logger.ErrorContext(ctx, "failed to rollback transaction", "error", rollbackErr)
			return errors.Join(err, rollbackErr)
		}

		return err
	}

	return tx.Commit(ctx)
}
