package gamelist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/storequery"
	application "retrom/internal/service/gamelist"
)

type Repository struct {
	database *sql.DB
}

func New(database *sql.DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) Detail(
	ctx context.Context, profileID, gameID string,
) (application.Detail, error) {
	var detail application.Detail
	var players, releaseYear sql.NullInt64
	var coverAssetID, videoAssetID sql.NullString
	err := repository.database.QueryRowContext(ctx, `
SELECT g.id,
g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year,
p.id,
p.name,
pi.id,
pi.name,
g.version,
g.updated_at_ms,
(SELECT a.id
FROM game_assets a
WHERE a.game_id=g.id
AND a.kind='COVER'
ORDER BY a.ordinal,
a.id
LIMIT 1),
(SELECT a.id
FROM game_assets a
WHERE a.game_id=g.id
AND a.kind='VIDEO'
AND a.ordinal=0
ORDER BY a.id
LIMIT 1),
COALESCE((SELECT SUM(active_duration_ms)
FROM play_sessions ps
WHERE ps.game_id=g.id
AND ps.profile_id=?),
0)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
`, profileID, gameID).Scan(
		&detail.GameID,
		&detail.Title,
		&detail.Description,
		&detail.Developer,
		&detail.Publisher,
		&detail.Genre,
		&players,
		&releaseYear,
		&detail.Platform.ID,
		&detail.Platform.Name,
		&detail.PlatformInstance.ID,
		&detail.PlatformInstance.Name,
		&detail.Version,
		&detail.UpdatedAtMS,
		&coverAssetID,
		&videoAssetID,
		&detail.ActiveDurationMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.Detail{}, application.ErrNotFound
	}
	if err != nil {
		return application.Detail{}, fmt.Errorf("query game detail: %w", err)
	}
	detail.Players = nullableInt64Pointer(players)
	detail.ReleaseYear = nullableInt64Pointer(releaseYear)
	detail.CoverAssetID = nullableStringPointer(coverAssetID)
	detail.VideoAssetID = nullableStringPointer(videoAssetID)

	detail.CoreOptions, err = repository.coreOptions(ctx, gameID)
	if err != nil {
		return application.Detail{}, err
	}
	detail.DOSEntries, err = repository.dosEntries(ctx, gameID)
	if err != nil {
		return application.Detail{}, err
	}
	detail.DefaultDOSEntry, err = repository.defaultDOSEntry(ctx, gameID)
	if err != nil {
		return application.Detail{}, err
	}
	detail.SaveStateCount, err = repository.saveStateCount(ctx, gameID, profileID)
	if err != nil {
		return application.Detail{}, err
	}
	detail.SaveStates, err = repository.recentSaveStates(ctx, gameID, profileID)
	if err != nil {
		return application.Detail{}, err
	}
	return detail, nil
}

func (repository *Repository) dosEntries(
	ctx context.Context, gameID string,
) ([]application.DOSEntry, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM dos_entries
WHERE game_id=?
ORDER BY rank,
normalized_path
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query DOS entries: %w", err)
	}
	defer func() { cleanup.Error("close DOS entries", rows.Close()) }()
	entries := make([]application.DOSEntry, 0)
	for rows.Next() {
		var entry application.DOSEntry
		var enabled, directLaunchSafe int
		if err := rows.Scan(
			&entry.Path,
			&entry.OriginalPath,
			&entry.Kind,
			&entry.Rank,
			&enabled,
			&directLaunchSafe,
		); err != nil {
			return nil, fmt.Errorf("scan DOS entry: %w", err)
		}
		entry.Enabled = enabled == 1
		entry.DirectLaunchSafe = directLaunchSafe == 1
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DOS entries: %w", err)
	}
	return entries, nil
}

func (repository *Repository) defaultDOSEntry(
	ctx context.Context, gameID string,
) (*string, error) {
	var entry sql.NullString
	err := repository.database.QueryRowContext(ctx, `
SELECT variant.default_dos_entry
FROM game_variants variant
WHERE variant.game_id=? AND variant.core_id='dosbox_pure'
`, gameID).Scan(&entry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // a game may have no DOS default entry
	}
	if err != nil {
		return nil, fmt.Errorf("query default DOS entry: %w", err)
	}
	return nullableStringPointer(entry), nil
}

func (repository *Repository) saveStateCount(
	ctx context.Context, gameID, profileID string,
) (int64, error) {
	var count int64
	if err := repository.database.QueryRowContext(ctx, `
SELECT count(*)
FROM save_states save
LEFT JOIN game_save_versions native ON native.save_state_id=save.id
JOIN (`+storequery.SaveRuntimeCompatibility+`) compatibility
  ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE save.game_id=?
AND save.profile_id=?
AND save.deleted_at_ms IS NULL
`, gameID, profileID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count game save states: %w", err)
	}
	return count, nil
}

func (repository *Repository) recentSaveStates(
	ctx context.Context, gameID, profileID string,
) ([]application.SaveState, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT s.id,
s.name,
s.created_at_ms,
native.last_synced_at_ms,
source_launch.core_id,
c.name,
s.disc_index,
s.screenshot_blob_id IS NOT NULL
FROM save_states s
LEFT JOIN game_save_versions native ON native.save_state_id=s.id
JOIN launch_sessions source_launch ON source_launch.id=s.source_launch_session_id
JOIN cores c ON c.id=source_launch.core_id
JOIN (`+storequery.SaveRuntimeCompatibility+`) compatibility
  ON compatibility.save_state_id=s.id AND compatibility.status='AVAILABLE'
WHERE s.game_id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
ORDER BY COALESCE(native.last_synced_at_ms,s.created_at_ms) DESC,
s.id DESC
LIMIT 8
`, gameID, profileID)
	if err != nil {
		return nil, fmt.Errorf("query recent game saves: %w", err)
	}
	defer func() { cleanup.Error("close recent game saves", rows.Close()) }()
	saves := make([]application.SaveState, 0)
	for rows.Next() {
		var save application.SaveState
		var lastSynced, discIndex sql.NullInt64
		if err := rows.Scan(
			&save.ID,
			&save.Name,
			&save.CreatedAtMS,
			&lastSynced,
			&save.CoreID,
			&save.CoreName,
			&discIndex,
			&save.HasScreenshot,
		); err != nil {
			return nil, fmt.Errorf("scan recent game save: %w", err)
		}
		save.LastSyncedAtMS = nullableInt64Pointer(lastSynced)
		save.DiscIndex = nullableInt64Pointer(discIndex)
		saves = append(saves, save)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent game saves: %w", err)
	}
	return saves, nil
}

func (repository *Repository) coreOptions(
	ctx context.Context, gameID string,
) ([]application.CoreOption, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT c.id,
c.name,
COALESCE(json_extract(bound_target.capabilities_json,'$.requiresThreads'),
 (SELECT max(json_extract(candidate_target.capabilities_json,'$.requiresThreads'))
  FROM runtime_target_bindings candidate
  JOIN runtime_binding_platforms candidate_platform ON candidate_platform.binding_id=candidate.binding_id
   AND candidate_platform.platform_id=pi.platform_id AND candidate_platform.core_id=c.id
  JOIN runtime_targets candidate_target ON candidate_target.provider_id=candidate.provider_id
   AND candidate_target.target_id=candidate.target_id
  WHERE candidate.core_id=c.id AND candidate.launch_policy<>'DISABLED'),0),
pi.default_core_id,
v.id,
v.provider_id,
v.target_id,
v.dat_version_id,
v.status,
v.compatibility_code
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platform_cores pc ON pc.platform_id=pi.platform_id
AND pc.enabled=1
JOIN cores c ON c.id=pc.core_id
AND c.enabled=1
LEFT JOIN game_variants v ON v.game_id=g.id
AND (v.core_id=c.id OR pi.platform_id='rpgmaker')
LEFT JOIN runtime_targets bound_target ON bound_target.provider_id=v.provider_id AND bound_target.target_id=v.target_id
WHERE g.id=?
ORDER BY c.name,
c.id
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query game core options: %w", err)
	}
	defer func() { cleanup.Error("close game core options", rows.Close()) }()
	options := make([]application.CoreOption, 0)
	for rows.Next() {
		var coreID, coreName, defaultCoreID string
		var requiresThreads int
		var variantID, providerID, targetID sql.NullString
		var datVersionID, status, compatibility sql.NullString
		if err := rows.Scan(
			&coreID,
			&coreName,
			&requiresThreads,
			&defaultCoreID,
			&variantID,
			&providerID,
			&targetID,
			&datVersionID,
			&status,
			&compatibility,
		); err != nil {
			return nil, fmt.Errorf("scan game core option: %w", err)
		}
		option := application.CoreOption{
			CoreID:             coreID,
			Name:               coreName,
			IsDefault:          coreID == defaultCoreID,
			RequiresThreads:    requiresThreads == 1,
			VariantID:          nullableStringPointer(variantID),
			ProviderID:         nullableStringPointer(providerID),
			TargetID:           nullableStringPointer(targetID),
			DATVersionID:       nullableStringPointer(datVersionID),
			RevalidationStatus: "NOT_REQUIRED",
		}
		switch {
		case variantID.Valid && status.String == "READY":
			option.Status = "READY"
			option.Reasons = []application.Reason{}
		case variantID.Valid && status.String == "BLOCKED" && compatibility.String == "VALIDATION_PENDING":
			option.Status = "NEEDS_VALIDATION"
			option.Reasons = []application.Reason{{Code: "VARIANT_VALIDATION_REQUIRED", Level: "INFO"}}
		case variantID.Valid && status.String == "BLOCKED":
			option.Status = "DEPENDENCY_MISSING"
			option.Reasons = []application.Reason{{Code: compatibility.String, Level: "BLOCKING"}}
		case variantID.Valid:
			option.Status = "INCOMPATIBLE"
			option.Reasons = []application.Reason{{Code: compatibility.String, Level: "BLOCKING"}}
		default:
			option.Status = "NEEDS_VALIDATION"
			option.Reasons = []application.Reason{{Code: "VARIANT_VALIDATION_REQUIRED", Level: "INFO"}}
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game core options: %w", err)
	}
	return options, nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func parseCursorInt(value string) (int64, error) {
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid game cursor timestamp: %w", err)
	}
	return result, nil
}

func withConditions(prefix string, conditions []string, suffix string) string {
	if len(conditions) == 0 {
		return prefix + suffix
	}
	return prefix + "\nWHERE " + strings.Join(conditions, "\nAND ") + suffix
}
