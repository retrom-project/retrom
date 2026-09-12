package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/netplay"
)

type Eligibility struct{ database *sql.DB }

func NewEligibility(database *sql.DB) *Eligibility { return &Eligibility{database: database} }
func (repository *Eligibility) GamePage(
	ctx context.Context,
	profileID, afterTitle, afterGameID string,
	limit int,
) ([]netplay.GameSummary, bool, error) {
	query := `
SELECT game.id,game.title,platform.id,platform.name,instance.id,instance.name,game.created_at_ms,
  (SELECT max(play.started_at_ms) FROM play_sessions play WHERE play.game_id=game.id AND play.profile_id=?),
  (SELECT asset.id FROM game_assets asset
   WHERE asset.game_id=game.id
     AND asset.kind='COVER' AND asset.ordinal=0 LIMIT 1)
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE game.status='PUBLISHED' AND instance.enabled=1`
	arguments := []any{profileID}
	if afterGameID != "" {
		query += ` AND (lower(game.title)>? OR (lower(game.title)=? AND game.id>?))`
		arguments = append(arguments, afterTitle, afterTitle, afterGameID)
	}
	query += ` ORDER BY lower(game.title),game.id LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, false, fmt.Errorf("netplay/list games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]netplay.GameSummary, 0, limit)
	for rows.Next() {
		var item netplay.GameSummary
		var coverID sql.NullString
		var lastPlayed sql.NullInt64
		if err := rows.Scan(
			&item.GameID, &item.Title, &item.PlatformID, &item.PlatformName,
			&item.PlatformInstanceID, &item.PlatformInstanceName, &item.AddedAtMS, &lastPlayed, &coverID,
		); err != nil {
			return nil, false, fmt.Errorf("netplay/scan game: %w", err)
		}
		if lastPlayed.Valid {
			item.LastPlayedAtMS = &lastPlayed.Int64
		}
		if coverID.Valid {
			value := "/content/assets/" + coverID.String
			item.CoverURL = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("netplay/list games: %w", err)
	}
	return items, len(items) == limit, nil
}

func (repository *Eligibility) Rows(ctx context.Context, gameID string) ([]netplay.EligibilityRow, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT variant.id,variant.provider_id,variant.target_id,provider.bundle_sha256,game.source_manifest_digest,
  platform.id,variant.core_id,core.name,variant.dependency_snapshot_json,game.content_kind,
  file.logical_name,variant.dat_version_id
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN game_variants variant ON variant.game_id=game.id AND variant.status='READY'
JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
 AND json_extract(target.capabilities_json,'$.netplayPort')=1
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.core_id=variant.core_id AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=platform.id AND binding_platform.core_id=variant.core_id
JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
 AND binding_kind.content_kind=game.content_kind
JOIN cores core ON core.id=variant.core_id
JOIN game_files file ON file.game_id=game.id AND file.role='CONTENT'
WHERE game.id=? AND game.status='PUBLISHED' AND instance.enabled=1
ORDER BY variant.core_id,variant.id,file.sort_order,file.logical_name
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("netplay/eligible profiles: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	lockedRows := make([]netplay.EligibilityRow, 0)
	for rows.Next() {
		var row netplay.EligibilityRow
		if err := rows.Scan(
			&row.VariantID, &row.ProviderID, &row.TargetID, &row.BundleSHA256, &row.SourceManifestDigest,
			&row.PlatformID, &row.CoreID, &row.CoreName,
			&row.DependencyJSON, &row.ContentKind, &row.LogicalName, &row.DATVersionID,
		); err != nil {
			return nil, fmt.Errorf("netplay/eligible profile row: %w", err)
		}
		lockedRows = append(lockedRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/eligible profiles: %w", err)
	}
	return lockedRows, nil
}
