package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
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

func (repository *RateLimits) CommitRecordRateLimit(
	ctx context.Context, cmd accounts.RecordRateLimitCommand,
) (accounts.RecordRateLimitResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return accounts.RecordRateLimitResult{}, fmt.Errorf("begin authentication rate limits: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := rateLimitRecords{tx}
	if err := records.Prune(ctx, cmd.NowMS-accounts.RateLimitWindow.Milliseconds(), cmd.NowMS); err != nil {
		return accounts.RecordRateLimitResult{}, fmt.Errorf("prune authentication limits: %w", err)
	}
	var maxRetry int64
	for _, entry := range cmd.Entries {
		current, found, err := records.Read(ctx, entry.Key)
		if err != nil {
			return accounts.RecordRateLimitResult{}, fmt.Errorf("read authentication failure bucket: %w", err)
		}
		if found && current.BlockedUntil != nil && *current.BlockedUntil > cmd.NowMS {
			if *current.BlockedUntil > maxRetry {
				maxRetry = *current.BlockedUntil
			}
			continue
		}
		next := accounts.NextRateLimitBucket(current, found, entry.Key, entry.Threshold, cmd.NowMS)
		if err := records.Write(ctx, next); err != nil {
			return accounts.RecordRateLimitResult{}, fmt.Errorf("record authentication failure bucket: %w", err)
		}
		if next.BlockedUntil != nil && *next.BlockedUntil > maxRetry {
			maxRetry = *next.BlockedUntil
		}
	}
	if err := tx.Commit(); err != nil {
		return accounts.RecordRateLimitResult{}, fmt.Errorf("commit authentication rate limit records: %w", err)
	}
	return accounts.RecordRateLimitResult{MaxRetryAfterMS: maxRetry}, nil
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
