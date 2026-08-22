package netplay

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/tagging"
)

type ProfileSummary struct {
	ID                string `json:"id"`
	CoreID            string `json:"coreId"`
	CoreName          string `json:"coreName"`
	EmulatorJSVersion string `json:"emulatorjsVersion"`
	MaxPlayers        int    `json:"maxPlayers"`
}

type GameSummary struct {
	GameID               string              `json:"gameId"`
	Title                string              `json:"title"`
	CoverURL             *string             `json:"coverUrl"`
	PlatformID           string              `json:"platformId"`
	PlatformName         string              `json:"platformName"`
	PlatformInstanceID   string              `json:"platformInstanceId"`
	PlatformInstanceName string              `json:"platformInstanceName"`
	LastPlayedAtMS       *int64              `json:"lastPlayedAtMs"`
	AddedAtMS            int64               `json:"addedAtMs"`
	Availability         string              `json:"availability"`
	NetplayProfiles      []ProfileSummary    `json:"netplayProfiles"`
	BlockerCode          *string             `json:"blockerCode"`
	Tags                 []tagging.Reference `json:"tags"`
}

type eligibleProfile struct {
	Summary                ProfileSummary
	Manifest               ManifestProfile
	VariantRevisionID      string
	CoreArtifactID         string
	DependencySnapshotJSON string
	DefaultCoreOptions     map[string]string
}

func (service *Service) Games(ctx context.Context, profileID, availability string) ([]GameSummary, error) {
	items := make([]GameSummary, 0)
	afterTitle, afterGameID := "", ""
	for {
		page, hasMore, err := service.GamePage(ctx, profileID, availability, afterTitle, afterGameID, 100)
		if err != nil {
			return nil, err
		}
		items = append(items, page...)
		if !hasMore {
			return items, nil
		}
		last := page[len(page)-1]
		afterTitle, afterGameID = strings.ToLower(last.Title), last.GameID
	}
}

func (service *Service) GamePage(
	ctx context.Context,
	profileID, availability, afterTitle, afterGameID string,
	limit int,
) ([]GameSummary, bool, error) {
	if availability == "" {
		availability = "SUPPORTED"
	}
	if (availability != "SUPPORTED" && availability != "ALL") || limit < 1 || limit > 100 ||
		(afterGameID == "") != (afterTitle == "") {
		return nil, false, ErrInvalidProfile
	}
	items := make([]GameSummary, 0, limit+1)
	scanTitle, scanGameID := afterTitle, afterGameID
	for len(items) <= limit {
		candidates, hasMoreCandidates, err := service.queryGamePage(
			ctx, profileID, scanTitle, scanGameID, limit+1,
		)
		if err != nil {
			return nil, false, err
		}
		items, scanTitle, scanGameID, err = service.appendEligibleGames(
			ctx, candidates, availability, items, limit,
		)
		if err != nil {
			return nil, false, err
		}
		if len(items) > limit {
			break
		}
		if !hasMoreCandidates {
			if err := service.attachGameTags(ctx, items); err != nil {
				return nil, false, err
			}
			return items, false, nil
		}
	}
	items = items[:limit]
	if err := service.attachGameTags(ctx, items); err != nil {
		return nil, false, err
	}
	return items, true, nil
}

func (service *Service) appendEligibleGames(
	ctx context.Context,
	candidates []GameSummary,
	availability string,
	items []GameSummary,
	limit int,
) ([]GameSummary, string, string, error) {
	lastTitle, lastGameID := "", ""
	for _, candidate := range candidates {
		lastTitle, lastGameID = strings.ToLower(candidate.Title), candidate.GameID
		item, include, err := service.enrichGame(ctx, candidate, availability)
		if err != nil {
			return nil, "", "", err
		}
		if include {
			items = append(items, item)
			if len(items) > limit {
				break
			}
		}
	}
	return items, lastTitle, lastGameID, nil
}

func (service *Service) queryGamePage(
	ctx context.Context,
	profileID, afterTitle, afterGameID string,
	limit int,
) ([]GameSummary, bool, error) {
	query := `
SELECT game.id,metadata.title,platform.id,platform.name,instance.id,instance.name,game.created_at_ms,
  (SELECT max(play.started_at_ms) FROM play_sessions play WHERE play.game_id=game.id AND play.profile_id=?),
  (SELECT asset.id FROM game_assets asset
   WHERE asset.game_id=game.id AND asset.metadata_revision_id=game.current_metadata_revision_id
     AND asset.kind='COVER' AND asset.ordinal=0 LIMIT 1)
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE game.status='PUBLISHED' AND instance.enabled=1`
	arguments := []any{profileID}
	if afterGameID != "" {
		query += ` AND (lower(metadata.title)>? OR (lower(metadata.title)=? AND game.id>?))`
		arguments = append(arguments, afterTitle, afterTitle, afterGameID)
	}
	query += ` ORDER BY lower(metadata.title),game.id LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, false, fmt.Errorf("netplay/list games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]GameSummary, 0, limit)
	for rows.Next() {
		var item GameSummary
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

func (service *Service) attachGameTags(ctx context.Context, items []GameSummary) error {
	gameIDs := make([]string, 0, len(items))
	for _, item := range items {
		gameIDs = append(gameIDs, item.GameID)
	}
	references, err := service.tags.References(ctx, gameIDs)
	if err != nil {
		return serviceError("list game tags", err)
	}
	for index := range items {
		items[index].Tags = references[items[index].GameID]
		if items[index].Tags == nil {
			items[index].Tags = []tagging.Reference{}
		}
	}
	return nil
}

func (service *Service) enrichGame(
	ctx context.Context,
	item GameSummary,
	availability string,
) (GameSummary, bool, error) {
	profiles, blocker, err := service.profileEligibility(ctx, item.GameID)
	if err != nil {
		return GameSummary{}, false, err
	}
	item.NetplayProfiles = make([]ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		item.NetplayProfiles = append(item.NetplayProfiles, profile.Summary)
	}
	if len(item.NetplayProfiles) > 0 {
		item.Availability = "SUPPORTED"
	} else {
		item.Availability = "UNSUPPORTED"
		item.BlockerCode = &blocker
	}
	return item, availability == "ALL" || item.Availability == "SUPPORTED", nil
}

func (service *Service) eligibleProfiles(ctx context.Context, gameID string) ([]eligibleProfile, error) {
	profiles, _, err := service.profileEligibility(ctx, gameID)
	return profiles, err
}

func eligibilityBlocker(hasVariant, contentKindAllowed, coreAllowed bool) string {
	if !hasVariant {
		return "GAME_UNAVAILABLE"
	}
	if !contentKindAllowed {
		return "CONTENT_NOT_ALLOWLISTED"
	}
	if !coreAllowed {
		return "CORE_NOT_ALLOWLISTED"
	}
	return "DEPENDENCY_STALE"
}

func (service *Service) dependencySnapshotCurrent(ctx context.Context, row eligibilityRow) (bool, error) {
	if row.datVersionID.Valid {
		return service.arcadeDependencySnapshotRunnable(ctx, row)
	}
	artifactID, logicalName, rawSnapshot := row.artifactID, row.logicalName, row.dependencyJSON
	lockedJSON, valid := lockedSnapshotJSON(rawSnapshot)
	if !valid {
		return false, nil
	}
	current, status, _, err := corevalidation.ResolveBIOS(ctx, service.database, artifactID, logicalName)
	if err != nil {
		return false, serviceError("resolve BIOS snapshot", err)
	}
	currentJSON, err := current.JSON()
	if err != nil {
		return false, serviceError("serialize BIOS snapshot", err)
	}
	return status == "READY" && bytes.Equal(lockedJSON, currentJSON), nil
}

type netplayArcadeClosureNode struct {
	Machine    string  `json:"machine"`
	Kind       string  `json:"kind"`
	RequiredBy *string `json:"requiredBy"`
	Depth      int     `json:"depth"`
}

type netplayArcadeDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type netplayArcadeSnapshot struct {
	SchemaVersion     int                        `json:"schemaVersion"`
	Machine           string                     `json:"machine"`
	DATVersionID      string                     `json:"datVersionId"`
	Closure           []netplayArcadeClosureNode `json:"closure"`
	Dependencies      []netplayArcadeDependency  `json:"dependencies"`
	MissingEntries    []string                   `json:"missingEntries"`
	MismatchedEntries []string                   `json:"mismatchedEntries"`
	Warnings          []string                   `json:"warnings"`
}

type netplayLockedArcadeDependency struct {
	state           string
	requiredEntries []string
}

func (service *Service) arcadeDependencySnapshotRunnable(ctx context.Context, row eligibilityRow) (bool, error) {
	snapshot, valid := parseNetplayArcadeSnapshot(row)
	if !valid {
		return false, nil
	}
	if !validArcadeRuntimeSnapshot(row.dependencyJSON) {
		return false, nil
	}
	closure, valid := netplayArcadeClosureIndex(snapshot)
	if !valid {
		return false, nil
	}
	locked, valid, err := service.loadNetplayArcadeDependencies(ctx, row)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, nil
	}
	if len(snapshot.Dependencies) != len(locked) || len(closure) != len(locked)+1 {
		return false, nil
	}
	return service.arcadeSnapshotDependenciesRunnable(ctx, row.revisionID, snapshot, closure, locked)
}

func validArcadeRuntimeSnapshot(raw string) bool {
	_, err := corevalidation.ParseRuntimeBIOSDependencies(raw)
	return err == nil
}

func parseNetplayArcadeSnapshot(row eligibilityRow) (netplayArcadeSnapshot, bool) {
	var snapshot netplayArcadeSnapshot
	decoder := json.NewDecoder(strings.NewReader(row.dependencyJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.SchemaVersion != 2 ||
		snapshot.DATVersionID != row.datVersionID.String || snapshot.Closure == nil || snapshot.Dependencies == nil ||
		snapshot.MissingEntries == nil || len(snapshot.MissingEntries) != 0 || snapshot.MismatchedEntries == nil ||
		len(snapshot.MismatchedEntries) != 0 || snapshot.Warnings == nil ||
		snapshot.Machine != strings.TrimSuffix(filepath.Base(row.logicalName), filepath.Ext(row.logicalName)) {
		return netplayArcadeSnapshot{}, false
	}
	return snapshot, true
}

func netplayArcadeClosureIndex(
	snapshot netplayArcadeSnapshot,
) (map[string]netplayArcadeClosureNode, bool) {
	closure := make(map[string]netplayArcadeClosureNode, len(snapshot.Closure))
	for _, node := range snapshot.Closure {
		key := node.Kind + "\x00" + node.Machine
		if node.Machine == "" || (node.Kind != "CONTENT" && node.Kind != "PARENT" && node.Kind != "BIOS_OR_BASE") {
			return nil, false
		}
		if _, duplicate := closure[key]; duplicate {
			return nil, false
		}
		closure[key] = node
	}
	root, exists := closure["CONTENT\x00"+snapshot.Machine]
	if !exists || root.Depth != 0 || root.RequiredBy != nil {
		return nil, false
	}
	return closure, true
}

func (service *Service) loadNetplayArcadeDependencies(
	ctx context.Context,
	row eligibilityRow,
) (map[string]netplayLockedArcadeDependency, bool, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT kind,logical_archive,source_machine_name,required_entries_json,state
FROM variant_dependencies
WHERE game_variant_revision_id=? AND dat_version_id=?
ORDER BY kind,logical_archive
	`, row.revisionID, row.datVersionID.String)
	if err != nil {
		return nil, false, serviceError("load Arcade dependencies", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	locked := make(map[string]netplayLockedArcadeDependency)
	for rows.Next() {
		var kind, logicalArchive, machine, requiredEntriesJSON, state string
		if err := rows.Scan(&kind, &logicalArchive, &machine, &requiredEntriesJSON, &state); err != nil {
			return nil, false, serviceError("scan Arcade dependency", err)
		}
		if logicalArchive != machine+".zip" {
			return nil, false, nil
		}
		var requiredEntries []string
		if err := json.Unmarshal([]byte(requiredEntriesJSON), &requiredEntries); err != nil || requiredEntries == nil {
			return nil, false, nil
		}
		key := kind + "\x00" + machine
		if _, duplicate := locked[key]; duplicate {
			return nil, false, nil
		}
		locked[key] = netplayLockedArcadeDependency{state: state, requiredEntries: requiredEntries}
	}
	if err := rows.Err(); err != nil {
		return nil, false, serviceError("iterate Arcade dependencies", err)
	}
	return locked, true, nil
}

func (service *Service) arcadeSnapshotDependenciesRunnable(
	ctx context.Context,
	revisionID string,
	snapshot netplayArcadeSnapshot,
	closure map[string]netplayArcadeClosureNode,
	locked map[string]netplayLockedArcadeDependency,
) (bool, error) {
	seenDependencies := make(map[string]struct{}, len(snapshot.Dependencies))
	expectedWarnings := make([]string, 0)
	for _, dependency := range snapshot.Dependencies {
		key := dependency.Kind + "\x00" + dependency.Machine
		node, inClosure := closure[key]
		stored, exists := locked[key]
		if _, duplicate := seenDependencies[key]; duplicate {
			return false, nil
		}
		seenDependencies[key] = struct{}{}
		if !inClosure || !exists || !netplayArcadeDependencyMatches(dependency, node, stored) {
			return false, nil
		}
		if stored.state == "HASH_WARNING" {
			expectedWarnings = append(expectedWarnings, dependency.Machine+".zip:HASH_WARNING")
		}
		if stored.state == "SATISFIED_EXTERNAL" || stored.state == "HASH_WARNING" {
			available, err := service.netplayArcadeDependencyFileAvailable(ctx, revisionID, dependency)
			if err != nil || !available {
				return false, err
			}
		}
	}
	sort.Strings(expectedWarnings)
	return slices.Equal(snapshot.Warnings, expectedWarnings), nil
}

func netplayArcadeDependencyMatches(
	dependency netplayArcadeDependency,
	node netplayArcadeClosureNode,
	stored netplayLockedArcadeDependency,
) bool {
	if node.Depth != dependency.Depth ||
		(node.RequiredBy == nil) != (dependency.RequiredBy == nil) ||
		node.RequiredBy != nil && *node.RequiredBy != *dependency.RequiredBy {
		return false
	}
	return dependency.ExpectedLogicalName == dependency.Machine+".zip" &&
		dependency.RequiredEntries != nil && dependency.RequiredEntryCount == len(dependency.RequiredEntries) &&
		slices.Equal(stored.requiredEntries, dependency.RequiredEntries) && stored.state == dependency.State &&
		(stored.state == "SATISFIED_BY_CONTENT" || stored.state == "SATISFIED_EXTERNAL" || stored.state == "HASH_WARNING")
}

func (service *Service) netplayArcadeDependencyFileAvailable(
	ctx context.Context,
	revisionID string,
	dependency netplayArcadeDependency,
) (bool, error) {
	role := "BIOS_BUNDLE"
	if dependency.Kind == "PARENT" {
		role = "PARENT"
	}
	var count int
	if err := service.database.QueryRowContext(ctx, `
SELECT count(*) FROM variant_files
WHERE game_variant_revision_id=? AND role=? AND logical_name=?
	`, revisionID, role, dependency.ExpectedLogicalName).Scan(&count); err != nil {
		return false, serviceError("check Arcade dependency file", err)
	}
	if count != 1 {
		return false, nil
	}
	return true, nil
}

func lockedSnapshotJSON(raw string) ([]byte, bool) {
	locked, err := corevalidation.ParseSnapshot(raw)
	if err != nil {
		return nil, false
	}
	encoded, err := locked.JSON()
	return encoded, err == nil
}

type eligibilityRow struct {
	revisionID, artifactID, coreID, coreName, emulatorVersion, artifactSHA string
	dependencyJSON, compatibilityJSON, contentKind, logicalName            string
	datVersionID                                                           sql.NullString
	artifactEnabled                                                        int
}

func (service *Service) profileEligibility(ctx context.Context, gameID string) ([]eligibleProfile, string, error) {
	lockedRows, err := service.queryEligibilityRows(ctx, gameID)
	if err != nil {
		return nil, "", err
	}
	result := make([]eligibleProfile, 0)
	seen := make(map[string]struct{})
	contentKindAllowed, coreAllowed := false, false
	for _, row := range lockedRows {
		for _, candidate := range service.registry.Profiles() {
			if _, duplicate := seen[candidate.ID]; duplicate {
				continue
			}
			profile, contentMatch, coreMatch, current, matchErr := service.matchEligibleProfile(ctx, row, candidate)
			contentKindAllowed = contentKindAllowed || contentMatch
			coreAllowed = coreAllowed || coreMatch
			if matchErr != nil {
				return nil, "", matchErr
			}
			if current {
				result = append(result, profile)
				seen[candidate.ID] = struct{}{}
			}
		}
	}
	slices.SortFunc(result, func(left, right eligibleProfile) int {
		return strings.Compare(left.Summary.ID, right.Summary.ID)
	})
	return result, eligibilityBlocker(len(lockedRows) > 0, contentKindAllowed, coreAllowed), nil
}

func (service *Service) queryEligibilityRows(ctx context.Context, gameID string) ([]eligibilityRow, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT revision.id,artifact.id,artifact.core_id,core.name,artifact.emulatorjs_version,artifact.sha256,
  revision.dependency_snapshot_json,artifact.compatibility_config_json,content.content_kind,
  file.logical_name,revision.dat_version_id,artifact.enabled
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
  AND revision.game_content_revision_id=game.current_content_revision_id AND revision.status='READY'
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
JOIN game_content_revisions content ON content.id=revision.game_content_revision_id
JOIN game_content_files file ON file.game_content_revision_id=content.id AND file.role='CONTENT'
WHERE game.id=? AND game.status='PUBLISHED'
ORDER BY artifact.core_id,revision.id,file.sort_order,file.logical_name
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("netplay/eligible profiles: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	lockedRows := make([]eligibilityRow, 0)
	for rows.Next() {
		var row eligibilityRow
		if err := rows.Scan(
			&row.revisionID, &row.artifactID, &row.coreID, &row.coreName, &row.emulatorVersion, &row.artifactSHA,
			&row.dependencyJSON, &row.compatibilityJSON, &row.contentKind, &row.logicalName, &row.datVersionID,
			&row.artifactEnabled,
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

func (service *Service) matchEligibleProfile(
	ctx context.Context,
	row eligibilityRow,
	candidate ManifestProfile,
) (eligibleProfile, bool, bool, bool, error) {
	contentKindAllowed, artifactMatches := service.matchesCoreProfile(row, candidate)
	if !contentKindAllowed {
		return eligibleProfile{}, false, false, false, nil
	}
	if !artifactMatches {
		return eligibleProfile{}, contentKindAllowed, false, false, nil
	}
	current, err := service.dependencySnapshotCurrent(ctx, row)
	if err != nil {
		return eligibleProfile{}, contentKindAllowed, true, false, fmt.Errorf("netplay/dependency snapshot: %w", err)
	}
	return eligibleProfile{
		Summary: ProfileSummary{
			ID: candidate.ID, CoreID: row.coreID, CoreName: row.coreName,
			EmulatorJSVersion: row.emulatorVersion, MaxPlayers: candidate.MaxPlayers,
		},
		Manifest: candidate, VariantRevisionID: row.revisionID, CoreArtifactID: row.artifactID,
		DependencySnapshotJSON: row.dependencyJSON, DefaultCoreOptions: compatibilityOptions(row.compatibilityJSON),
	}, contentKindAllowed, true, current, nil
}

func (service *Service) matchesCoreProfile(row eligibilityRow, candidate ManifestProfile) (bool, bool) {
	contentKindAllowed := slices.Contains(service.registry.Manifest.Protocol.AllowedContentKinds, row.contentKind)
	artifactMatches := contentKindAllowed && row.artifactEnabled == 1 && candidate.CoreID == row.coreID &&
		candidate.EmulatorJSVersion == row.emulatorVersion && candidate.CoreArtifactSHA256 == row.artifactSHA
	return contentKindAllowed, artifactMatches
}

func compatibilityOptions(raw string) map[string]string {
	var compatibility struct {
		DefaultOptions map[string]string `json:"defaultOptions"`
	}
	if json.Unmarshal([]byte(raw), &compatibility) != nil || compatibility.DefaultOptions == nil {
		return map[string]string{}
	}
	return compatibility.DefaultOptions
}
