package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
)

const maximumRuntimePackFiles = 20_000

type runtimePackFileIndex struct {
	Files         []runtimePackFileIndexEntry `json:"files"`
	SchemaVersion int                         `json:"schemaVersion"`
}

type runtimePackFileIndexEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	URL       string `json:"url"`
}

type RuntimePackFileView struct {
	Digest    string
	SizeBytes int64
}

func (service *Service) RuntimePackIndex(
	ctx context.Context,
	launchID, capability string,
	slot int,
) (ProjectIndexView, error) {
	source, err := service.authorizedRPGConfigSource(ctx, launchID, capability)
	if err != nil || source.runtimeKind != "EASYRPG_WEB" || slot < 0 || slot > 2 {
		return ProjectIndexView{}, ErrCredential
	}
	installationID, err := service.runtimePackInstallation(ctx, launchID, slot)
	if err != nil {
		return ProjectIndexView{}, err
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT path,size_bytes FROM runtime_asset_pack_files
WHERE installation_id=? ORDER BY ordinal
`, installationID)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load runtime pack index: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack index", rows.Close()) }()
	entries := make([]runtimePackFileIndexEntry, 0)
	for rows.Next() {
		var entry runtimePackFileIndexEntry
		if err := rows.Scan(&entry.Path, &entry.SizeBytes); err != nil || len(entries) >= maximumRuntimePackFiles {
			return ProjectIndexView{}, ErrCredential
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, fmt.Errorf("read runtime pack index: %w", err)
	}
	return buildRuntimePackIndex(launchID, slot, entries)
}

func buildRuntimePackIndex(
	launchID string,
	slot int,
	entries []runtimePackFileIndexEntry,
) (ProjectIndexView, error) {
	if launchID == "" || slot < 0 || slot > 2 || len(entries) < 1 || len(entries) > maximumRuntimePackFiles {
		return ProjectIndexView{}, ErrCredential
	}
	seen := make(map[string]struct{}, len(entries))
	for index := range entries {
		normalized, pathErr := importing.ValidateLogicalPath(entries[index].Path)
		folded := importing.ASCIICaseFold(normalized)
		_, duplicate := seen[folded]
		if pathErr != nil || normalized != entries[index].Path || entries[index].SizeBytes < 1 || duplicate {
			return ProjectIndexView{}, ErrCredential
		}
		seen[folded] = struct{}{}
		entries[index].URL = fmt.Sprintf(
			"/runtime/projects/%s/__retrom__/packs/%d/files/%s",
			launchID, slot, escapeProjectPath(entries[index].Path),
		)
	}
	contents, err := json.Marshal(runtimePackFileIndex{Files: entries, SchemaVersion: 1})
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("marshal runtime pack index: %w", err)
	}
	digest := sha256.Sum256(contents)
	return ProjectIndexView{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}

func (service *Service) RuntimePackFile(
	ctx context.Context,
	launchID, capability string,
	slot int,
	logicalName string,
) (RuntimePackFileView, error) {
	source, err := service.authorizedRPGConfigSource(ctx, launchID, capability)
	if err != nil || source.runtimeKind != "EASYRPG_WEB" || slot < 0 || slot > 2 {
		return RuntimePackFileView{}, ErrCredential
	}
	normalized, err := importing.ValidateLogicalPath(logicalName)
	if err != nil || normalized != logicalName {
		return RuntimePackFileView{}, ErrCredential
	}
	installationID, err := service.runtimePackInstallation(ctx, launchID, slot)
	if err != nil {
		return RuntimePackFileView{}, err
	}
	var view RuntimePackFileView
	err = service.database.QueryRowContext(ctx, `
SELECT sha256,size_bytes FROM runtime_asset_pack_files
WHERE installation_id=? AND path=?
`, installationID, logicalName).Scan(&view.Digest, &view.SizeBytes)
	if err != nil || view.SizeBytes < 1 || len(view.Digest) != 64 {
		return RuntimePackFileView{}, ErrCredential
	}
	return view, nil
}

func (service *Service) runtimePackInstallation(
	ctx context.Context,
	launchID string,
	slot int,
) (string, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT selection.installation_id
FROM launch_sessions launch
JOIN game_variant_revision_runtime_packs selection
  ON selection.game_variant_revision_id=launch.game_variant_revision_id
JOIN runtime_asset_pack_installations installation
  ON installation.id=selection.installation_id AND installation.status='READY'
JOIN launch_content_files locked ON locked.launch_session_id=launch.id
  AND locked.blob_id=installation.bundle_blob_id
WHERE launch.id=? AND launch.purpose='PRODUCT' AND selection.slot=?
UNION ALL
SELECT selection.installation_id
FROM launch_sessions launch
JOIN rpgmaker_runtime_validations validation
  ON validation.id=launch.rpgmaker_runtime_validation_id
JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
JOIN review_draft_runtime_pack_selections selection ON selection.review_draft_id=draft.id
JOIN runtime_asset_pack_installations installation
  ON installation.id=selection.installation_id AND installation.status='READY'
JOIN launch_content_files locked ON locked.launch_session_id=launch.id
  AND locked.blob_id=installation.bundle_blob_id
WHERE launch.id=? AND launch.purpose='RPG_RUNTIME_VALIDATION' AND selection.slot=?
`, launchID, slot, launchID, slot)
	if err != nil {
		return "", fmt.Errorf("load runtime pack selection: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack selection", rows.Close()) }()
	var installationID string
	if !rows.Next() || rows.Scan(&installationID) != nil || installationID == "" || rows.Next() {
		return "", ErrCredential
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read runtime pack selection: %w", err)
	}
	return installationID, nil
}
