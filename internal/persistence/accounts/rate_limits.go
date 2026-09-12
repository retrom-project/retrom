package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/service/accounts"
)

type (
	RateLimits       struct{ database *sql.DB }
	rateLimitRecords struct{ executor dbexec.Executor }
)

func NewRateLimits(database *sql.DB) *RateLimits { return &RateLimits{database} }
func (repository *RateLimits) Read(
	ctx context.Context,
	key accounts.RateLimitKey,
) (accounts.RateLimitBucket, bool, error) {
	return (rateLimitRecords{repository.database}).Read(ctx, key)
}

func (repository *RateLimits) Clear(ctx context.Context, key accounts.RateLimitKey) error {
	_, err := repository.database.ExecContext(
		ctx,
		`DELETE FROM auth_rate_limits WHERE scope=? AND subject_hash=?`,
		key.Scope,
		key.Digest[:],
	)
	if err != nil {
		return fmt.Errorf("delete authentication rate limit: %w", err)
	}
	return nil
}

func (repository *RateLimits) WithWrite(ctx context.Context, work func(accounts.RateLimitRecords) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin authentication rate limits: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(rateLimitRecords{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authentication rate limit records: %w", err)
	}
	return nil
}

func (records rateLimitRecords) Read(
	ctx context.Context,
	key accounts.RateLimitKey,
) (accounts.RateLimitBucket, bool, error) {
	value := accounts.RateLimitBucket{Key: key}
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT window_started_at_ms,failure_count,blocked_until_ms,updated_at_ms
 FROM auth_rate_limits WHERE scope=? AND subject_hash=?`,
		key.Scope,
		key.Digest[:],
	).Scan(
		&value.WindowStarted,
		&value.Failures,
		&value.BlockedUntil,
		&value.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return value, false, nil
	}
	if err != nil {
		return value, false, fmt.Errorf("query authentication rate limit: %w", err)
	}
	return value, true, nil
}

func (records rateLimitRecords) Write(ctx context.Context, value accounts.RateLimitBucket) error {
	_, err := records.executor.ExecContext(ctx, `INSERT INTO auth_rate_limits
 (scope,subject_hash,window_started_at_ms,failure_count,blocked_until_ms,updated_at_ms) VALUES(?,?,?,?,?,?)
 ON CONFLICT(scope,subject_hash) DO UPDATE SET window_started_at_ms=excluded.window_started_at_ms,
 failure_count=excluded.failure_count,blocked_until_ms=excluded.blocked_until_ms,updated_at_ms=excluded.updated_at_ms`,
		value.Key.Scope, value.Key.Digest[:], value.WindowStarted, value.Failures, value.BlockedUntil, value.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert authentication rate limit: %w", err)
	}
	return nil
}

func (records rateLimitRecords) Prune(ctx context.Context, olderThan, now int64) error {
	_, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM auth_rate_limits WHERE (scope,subject_hash) IN (
 SELECT scope,subject_hash FROM auth_rate_limits WHERE updated_at_ms<?
 AND (blocked_until_ms IS NULL OR blocked_until_ms<=?) ORDER BY updated_at_ms,scope,subject_hash LIMIT 100)`,
		olderThan,
		now,
	)
	if err != nil {
		return fmt.Errorf("prune authentication rate limits: %w", err)
	}
	return nil
}
