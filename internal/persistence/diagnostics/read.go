package diagnostics

import (
	"context"
	"fmt"

	application "retrom/internal/service/diagnostics"
)

func (reader records) SchemaVersion(ctx context.Context) (int64, error) {
	var version int64
	if err := reader.executor.QueryRowContext(ctx, `
SELECT COALESCE(MAX(version),0)
FROM schema_migrations
`).Scan(&version); err != nil {
		return 0, fmt.Errorf("diagnostics: read schema version: %w", err)
	}
	return version, nil
}

func (reader records) Counts(ctx context.Context) (application.Counts, error) {
	var counts application.Counts
	err := reader.executor.QueryRowContext(ctx, `
SELECT
(SELECT count(*) FROM games WHERE status='PUBLISHED'),
(SELECT count(*) FROM games WHERE status='DELETED'),
(SELECT count(*) FROM save_states WHERE deleted_at_ms IS NULL),
(SELECT count(*) FROM save_states WHERE deleted_at_ms IS NOT NULL),
(SELECT count(*) FROM blobs),
(SELECT count(*) FROM jobs WHERE state='QUEUED'),
(SELECT count(*) FROM jobs WHERE state='RUNNING'),
(SELECT count(*) FROM jobs WHERE state='CANCEL_REQUESTED'),
(SELECT count(*) FROM jobs WHERE state='SUCCEEDED'),
(SELECT count(*) FROM jobs WHERE state='FAILED'),
(SELECT count(*) FROM jobs WHERE state='CANCELLED'),
(SELECT count(*) FROM dat_versions WHERE parse_status='PENDING'),
(SELECT count(*) FROM dat_versions WHERE parse_status='PARSING'),
(SELECT count(*) FROM dat_versions WHERE parse_status='READY'),
(SELECT count(*) FROM dat_versions WHERE parse_status='FAILED'),
(SELECT count(*) FROM dat_versions WHERE parse_status='CANCELLED')
`).Scan(
		&counts.PublishedGames, &counts.DeletedGames, &counts.ActiveSaves, &counts.DeletedSaves,
		&counts.Blobs, &counts.QueuedJobs, &counts.RunningJobs, &counts.CancelRequestedJobs,
		&counts.SucceededJobs, &counts.FailedJobs, &counts.CancelledJobs,
		&counts.PendingDATs, &counts.ParsingDATs, &counts.ReadyDATs, &counts.FailedDATs,
		&counts.CancelledDATs,
	)
	if err != nil {
		return application.Counts{}, fmt.Errorf("diagnostics: read counts: %w", err)
	}
	return counts, nil
}

func (reader records) RuntimeProviders(ctx context.Context) ([]application.RuntimeProvider, error) {
	rows, err := reader.executor.QueryContext(ctx, `
SELECT provider_id,provider_version,bundle_sha256,source
FROM runtime_providers ORDER BY provider_id
`)
	if err != nil {
		return nil, fmt.Errorf("diagnostics: query runtime providers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	providers := make([]application.RuntimeProvider, 0, 2)
	for rows.Next() {
		var provider application.RuntimeProvider
		if err := rows.Scan(
			&provider.ProviderID, &provider.ProviderVersion, &provider.BundleSHA256, &provider.Source,
		); err != nil {
			return nil, fmt.Errorf("diagnostics: scan runtime provider: %w", err)
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("diagnostics: iterate runtime providers: %w", err)
	}
	return providers, nil
}

var _ application.ReadScope = records{}
