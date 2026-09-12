package runtimeprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	service "retrom/internal/service/runtimeprovider"
)

func (records catalogRecords) Current(ctx context.Context) (service.CurrentState, error) {
	providers, err := loadCurrentProviders(ctx, records.executor)
	if err != nil {
		return service.CurrentState{}, err
	}
	targets, err := records.targets(ctx)
	if err != nil {
		return service.CurrentState{}, err
	}
	var digest string
	err = records.executor.QueryRowContext(
		ctx,
		`SELECT catalog_sha256 FROM runtime_catalog_state WHERE singleton=1`,
	).Scan(
		&digest,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return service.CurrentState{}, fmt.Errorf("runtimeprovider/read catalog digest: %w", err)
	}
	return service.CurrentState{Providers: providers, Targets: targets, CatalogSHA256: digest}, nil
}

func (records catalogRecords) targets(ctx context.Context) ([]service.TargetIdentity, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		`SELECT provider_id,target_id FROM runtime_targets ORDER BY provider_id,target_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("runtimeprovider/read targets: %w", err)
	}
	defer func() { cleanup.Error("close runtime targets", rows.Close()) }()
	var result []service.TargetIdentity
	for rows.Next() {
		var target service.TargetIdentity
		if err := rows.Scan(&target.ProviderID, &target.TargetID); err != nil {
			return nil, fmt.Errorf("runtimeprovider/scan target: %w", err)
		}
		result = append(result, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimeprovider/iterate targets: %w", err)
	}
	return result, nil
}

func (records catalogRecords) CheckpointFormats(ctx context.Context, target service.TargetIdentity) ([]string, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT DISTINCT save.checkpoint_format FROM save_states save
JOIN launch_sessions source_launch ON source_launch.id=save.source_launch_session_id
JOIN game_variants variant ON variant.game_id=save.game_id AND variant.core_id=source_launch.core_id
WHERE save.deleted_at_ms IS NULL AND variant.provider_id=? AND variant.target_id=?
`, target.ProviderID, target.TargetID)
	if err != nil {
		return nil, fmt.Errorf("runtimeprovider/read checkpoint formats: %w", err)
	}
	defer func() { cleanup.Error("close checkpoint formats", rows.Close()) }()
	var result []string
	for rows.Next() {
		var format string
		if err := rows.Scan(&format); err != nil {
			return nil, fmt.Errorf("runtimeprovider/scan checkpoint format: %w", err)
		}
		result = append(result, format)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtimeprovider/iterate checkpoint formats: %w", err)
	}
	return result, nil
}
