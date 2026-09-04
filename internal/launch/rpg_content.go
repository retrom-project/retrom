package launch

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/nativeweb"
)

const (
	rpgProjectFormat         = "RPG_MAKER_PROJECT"
	rpgEasyIndexName         = "__retrom__/index.json"
	rpgMKXPArchiveName       = "__retrom__/game.mkxpz"
	rpgMKXPArchivePublicName = "game.mkxpz"
)

func (service *Service) buildRPGProductContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	if selection.contentKind != rpgProjectFormat {
		return launchContentPlan{}, ErrBlocked
	}
	files, err := queryLockedContentFiles(ctx, service.database, `
SELECT file.blob_id,file.logical_name,'PROJECT_FILE'
FROM game_files file
WHERE file.game_id=? AND file.role='PROJECT_FILE'
UNION ALL
SELECT file.blob_id,file.logical_name,file.role
FROM variant_files file
WHERE file.game_variant_id=?
  AND file.role IN ('RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE')
UNION ALL
SELECT installation.bundle_blob_id,selection.declared_name,'RPG_RUNTIME_PACK:' || selection.slot
FROM game_variant_runtime_packs selection
JOIN runtime_asset_pack_installations installation ON installation.id=selection.installation_id
WHERE selection.game_variant_id=? AND installation.status='READY'
ORDER BY 2
`, selection.gameID, selection.variantID, selection.variantID)
	if err != nil {
		return launchContentPlan{}, err
	}
	requiredRole, nativeRuntime, err := requiredRPGContent(selection.deliveryProfile)
	if err != nil {
		return launchContentPlan{}, err
	}
	return makeRPGContentPlan(files, requiredRole, nativeRuntime)
}

func (service *Service) buildRPGValidationContentPlan(
	ctx context.Context,
	queryer interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	validationID, sourceSnapshotID, deliveryProfile string,
) (launchContentPlan, error) {
	files, err := queryLockedContentFiles(ctx, queryer, `
SELECT file.blob_id,file.logical_name,'PROJECT_FILE'
FROM import_item_source_snapshot_files file
WHERE file.source_snapshot_id=? AND file.role='PROJECT_FILE'
UNION ALL
SELECT file.blob_id,file.logical_name,file.role
FROM rpgmaker_runtime_validations validation
JOIN import_item_core_validations core_validation ON core_validation.id=(
  SELECT candidate.id
  FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=validation.import_item_id
    AND candidate.source_snapshot_id=validation.effective_source_snapshot_id
    AND candidate.provider_id=validation.provider_id
    AND candidate.target_id=validation.target_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC
  LIMIT 1
)
JOIN import_item_validation_files file
  ON file.import_item_core_validation_id=core_validation.id
WHERE validation.id=? AND validation.effective_source_snapshot_id=?
  AND file.role IN ('RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE')
UNION ALL
SELECT installation.bundle_blob_id,selection.declared_name,'RPG_RUNTIME_PACK:' || selection.slot
FROM rpgmaker_runtime_validations validation
JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
JOIN review_draft_runtime_pack_selections selection ON selection.review_draft_id=draft.id
JOIN runtime_asset_pack_installations installation ON installation.id=selection.installation_id
WHERE validation.id=? AND validation.effective_source_snapshot_id=? AND installation.status='READY'
ORDER BY 2
`, sourceSnapshotID, validationID, sourceSnapshotID, validationID, sourceSnapshotID)
	if err != nil {
		return launchContentPlan{}, err
	}
	requiredRole, nativeRuntime, err := requiredRPGContent(deliveryProfile)
	if err != nil {
		return launchContentPlan{}, err
	}
	return makeRPGContentPlan(files, requiredRole, nativeRuntime)
}

func requiredRPGContent(deliveryProfile string) (string, bool, error) {
	switch deliveryProfile {
	case "FILE_TREE_PROJECT":
		return "RPG_EASYRPG_INDEX", false, nil
	case "SEEKABLE_PROJECT_ARCHIVE":
		return "RPG_MAKER_LAUNCH_BUNDLE", false, nil
	case "ISOLATED_WEB_PROJECT":
		return "", true, nil
	default:
		return "", false, ErrBlocked
	}
}

func copyRPGValidationContentPlan(
	ctx context.Context,
	queryer interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	originalLaunchID string,
) (launchContentPlan, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT blob_id,logical_name,format_version FROM launch_content_files
WHERE launch_session_id=? ORDER BY logical_name
`, originalLaunchID)
	if err != nil {
		return launchContentPlan{}, fmt.Errorf("load original RPG validation content: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]lockedContentFile, 0)
	for rows.Next() {
		var file lockedContentFile
		if err := rows.Scan(&file.BlobID, &file.LogicalName, &file.Format); err != nil ||
			file.Format != rpgProjectFormat || !validRPGProjectPath(file.LogicalName) {
			return launchContentPlan{}, ErrBlocked
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil || len(files) == 0 || len(files) > 10_006 {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: rpgProjectFormat, Files: files}, nil
}

type rpgLockedFile struct {
	blobID      string
	logicalName string
	role        string
}

type rpgContentPlanBuilder struct {
	locked        []lockedContentFile
	seen          map[string]struct{}
	projectFiles  int
	nativeEntries int
	requiredFiles int
}

func queryLockedContentFiles(
	ctx context.Context,
	queryer interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	query string,
	arguments ...any,
) ([]rpgLockedFile, error) {
	rows, err := queryer.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load RPG Maker launch content: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]rpgLockedFile, 0)
	for rows.Next() {
		var file rpgLockedFile
		if err := rows.Scan(&file.blobID, &file.logicalName, &file.role); err != nil {
			return nil, fmt.Errorf("scan RPG Maker launch content: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read RPG Maker launch content: %w", err)
	}
	return files, nil
}

func makeRPGContentPlan(
	files []rpgLockedFile,
	requiredRole string,
	nativeRuntime bool,
) (launchContentPlan, error) {
	if len(files) == 0 || len(files) > 10_006 {
		return launchContentPlan{}, ErrBlocked
	}
	builder := rpgContentPlanBuilder{
		locked: make([]lockedContentFile, 0, len(files)),
		seen:   make(map[string]struct{}, len(files)),
	}
	for _, file := range files {
		if err := builder.add(file, requiredRole, nativeRuntime); err != nil {
			return launchContentPlan{}, ErrBlocked
		}
	}
	if builder.projectFiles == 0 || builder.projectFiles > 10_000 {
		return launchContentPlan{}, ErrBlocked
	}
	if nativeRuntime && builder.nativeEntries != 1 {
		return launchContentPlan{}, ErrBlocked
	}
	if requiredRole != "" && builder.requiredFiles != 1 {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: rpgProjectFormat, Files: builder.locked}, nil
}

func (builder *rpgContentPlanBuilder) add(
	file rpgLockedFile,
	requiredRole string,
	nativeRuntime bool,
) error {
	logicalName, project, valid := rpgLockedLogicalName(file)
	if !valid {
		return ErrBlocked
	}
	if !includeRPGContentFile(project, nativeRuntime, logicalName) {
		return nil
	}
	if project {
		builder.projectFiles++
		if isNativeRPGEntry(nativeRuntime, logicalName) {
			builder.nativeEntries++
		}
	}
	if file.role == requiredRole && requiredRole != "" {
		builder.requiredFiles++
	}
	if !validRPGProjectPath(logicalName) {
		return ErrBlocked
	}
	if _, duplicate := builder.seen[logicalName]; duplicate {
		return ErrBlocked
	}
	builder.seen[logicalName] = struct{}{}
	builder.locked = append(builder.locked, lockedContentFile{
		BlobID: file.blobID, LogicalName: logicalName, Format: rpgProjectFormat,
	})
	return nil
}

func includeRPGContentFile(project, nativeRuntime bool, logicalName string) bool {
	return !project || !nativeRuntime || nativeweb.RuntimeFile(logicalName)
}

func isNativeRPGEntry(nativeRuntime bool, logicalName string) bool {
	return nativeRuntime && logicalName == "index.html"
}

func rpgLockedLogicalName(file rpgLockedFile) (string, bool, bool) {
	switch file.role {
	case "PROJECT_FILE":
		return file.logicalName, true, !strings.HasPrefix(file.logicalName, "__retrom__/")
	case "RPG_EASYRPG_INDEX":
		return rpgEasyIndexName, false, true
	case "RPG_MAKER_LAUNCH_BUNDLE":
		return rpgMKXPArchiveName, false, true
	default:
		return rpgPackLogicalName(file.role)
	}
}

func rpgPackLogicalName(role string) (string, bool, bool) {
	if !strings.HasPrefix(role, "RPG_RUNTIME_PACK:") {
		return "", false, false
	}
	slot := strings.TrimPrefix(role, "RPG_RUNTIME_PACK:")
	if len(slot) != 1 || slot < "0" || slot > "3" {
		return "", false, false
	}
	return "__retrom__/pack-" + slot + ".zip", false, true
}

func validRPGProjectPath(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) &&
		path.Clean(value) == value && !path.IsAbs(value) &&
		value != "." && !strings.HasPrefix(value, "../") &&
		!strings.Contains(value, "\\") && !strings.ContainsRune(value, 0)
}
