package persistence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"retrom/internal/model"
	"retrom/migrations"
)

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	pool *pgxpool.Pool
	db   DB
}

var ErrCommitUncertain = errors.New("transaction commit result is uncertain")

func Open(ctx context.Context, url string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	return &Repository{pool: pool, db: pool}, nil
}

func (r *Repository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func (r *Repository) Migrate(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer rollback(ctx, tx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(145678324)"); err != nil {
		return fmt.Errorf("lock migration: %w", err)
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations_tab (id TEXT PRIMARY KEY)"); err != nil {
		return fmt.Errorf("create migration bookkeeping: %w", err)
	}
	var applied bool
	query := "SELECT EXISTS(SELECT 1 FROM schema_migrations_tab WHERE id='001_schema')"
	if err = tx.QueryRow(ctx, query).Scan(&applied); err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	if !applied {
		if _, err = tx.Exec(ctx, migrations.Schema); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations_tab(id) VALUES('001_schema')"); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	}
	if err = applyMigration(ctx, tx, "002_query_indexes", migrations.QueryIndexes); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func (r *Repository) Transaction(ctx context.Context, operation func(*Repository) error) error {
	if r.pool == nil {
		return operation(r)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer rollback(ctx, tx)
	if err = operation(&Repository{db: tx}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return fmt.Errorf("commit transaction: %w", err)
		}
		return fmt.Errorf("commit transaction: %w", errors.Join(ErrCommitUncertain, err))
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("rollback transaction", "error", err)
	}
}

func failure(action string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", action, model.ErrNotFound)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return fmt.Errorf("%s: %w", action, model.ErrConflict)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func applyMigration(ctx context.Context, tx pgx.Tx, id, schema string) error {
	var applied bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations_tab WHERE id=$1)",
		id).Scan(&applied); err != nil {
		return fmt.Errorf("read migration %s: %w", id, err)
	}
	if applied {
		return nil
	}
	if _, err := tx.Exec(ctx, schema); err != nil {
		return fmt.Errorf("apply migration %s: %w", id, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations_tab(id) VALUES($1)", id); err != nil {
		return fmt.Errorf("record migration %s: %w", id, err)
	}
	return nil
}
