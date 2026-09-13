package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/gamecontent"
)

func retirementReferenceQuery(kind gamecontent.RetirementReferenceKind) string {
	switch kind {
	case gamecontent.RetirementSave:
		return `SELECT game_id,id,'',payload_blob_id,screenshot_blob_id FROM save_states
WHERE game_id=? ORDER BY id LIMIT ?`
	case gamecontent.RetirementLaunchContent:
		return `SELECT file.launch_session_id,file.logical_name,'',file.blob_id,NULL FROM launch_content_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
ORDER BY file.launch_session_id,file.logical_name LIMIT ?`
	case gamecontent.RetirementLaunchExternal:
		return `SELECT file.launch_session_id,file.virtual_path,'',file.blob_id,NULL FROM launch_external_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
ORDER BY file.launch_session_id,file.virtual_path LIMIT ?`
	case gamecontent.RetirementVariantFile:
		return `SELECT file.game_variant_id,file.logical_name,file.role,file.blob_id,NULL FROM variant_files file
JOIN game_variants variant ON variant.id=file.game_variant_id WHERE variant.game_id=?
ORDER BY file.game_variant_id,file.role,file.logical_name LIMIT ?`
	case gamecontent.RetirementVariantDependency:
		return `SELECT dependency.game_variant_id,dependency.logical_archive,dependency.kind,NULL,NULL
FROM variant_dependencies dependency JOIN game_variants variant ON variant.id=dependency.game_variant_id
WHERE variant.game_id=? ORDER BY dependency.game_variant_id,dependency.kind,dependency.logical_archive LIMIT ?`
	default:
		return ""
	}
}

func (records retirementRecords) References(
	ctx context.Context, gameID string, kind gamecontent.RetirementReferenceKind, limit int,
) ([]gamecontent.RetirementReference, error) {
	query := retirementReferenceQuery(kind)
	if query == "" || limit < 1 || limit > 200 {
		return nil, gamecontent.ErrInvalid
	}
	rows, err := records.executor.QueryContext(ctx, query, gameID, limit)
	if err != nil {
		return nil, fmt.Errorf("query replacement references: %w", err)
	}
	defer func() { cleanup.Error("close retirement rows", rows.Close()) }()
	var result []gamecontent.RetirementReference
	for rows.Next() {
		var reference gamecontent.RetirementReference
		var blob, extra sql.NullString
		if err := rows.Scan(&reference.OwnerID, &reference.Key, &reference.Qualifier, &blob, &extra); err != nil {
			return nil, fmt.Errorf("scan replacement reference: %w", err)
		}
		reference.BlobID, reference.ExtraBlobID = blob.String, extra.String
		result = append(result, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replacement references: %w", err)
	}
	return result, nil
}

func (records retirementRecords) Remove(
	ctx context.Context, gameID string, kind gamecontent.RetirementReferenceKind,
	refs []gamecontent.RetirementReference,
) error {
	for _, ref := range refs {
		if err := records.remove(ctx, gameID, kind, ref); err != nil {
			return err
		}
	}
	return nil
}

func (records retirementRecords) remove(
	ctx context.Context, gameID string, kind gamecontent.RetirementReferenceKind,
	ref gamecontent.RetirementReference,
) error {
	switch kind {
	case gamecontent.RetirementSave:
		return requireChanged(recordstore.DeleteSaveStates(ctx, records.executor, recordstore.Scope{
			Where: `id=? AND game_id=? AND payload_blob_id=? AND COALESCE(screenshot_blob_id,'')=?`,
			Args:  []any{ref.Key, gameID, ref.BlobID, ref.ExtraBlobID},
		}))
	case gamecontent.RetirementLaunchContent:
		return requireChanged(recordstore.DeleteLaunchContentFiles(ctx, records.executor, recordstore.Scope{
			Where: `launch_session_id=? AND logical_name=? AND blob_id=?
AND launch_session_id IN (SELECT id FROM launch_sessions WHERE game_id=?)`,
			Args: []any{ref.OwnerID, ref.Key, ref.BlobID, gameID},
		}))
	case gamecontent.RetirementLaunchExternal:
		return requireChanged(recordstore.DeleteLaunchExternalFiles(ctx, records.executor, recordstore.Scope{
			Where: `launch_session_id=? AND virtual_path=? AND blob_id=?
AND launch_session_id IN (SELECT id FROM launch_sessions WHERE game_id=?)`,
			Args: []any{ref.OwnerID, ref.Key, ref.BlobID, gameID},
		}))
	case gamecontent.RetirementVariantFile:
		return requireChanged(recordstore.DeleteVariantFiles(ctx, records.executor, recordstore.Scope{
			Where: `game_variant_id=? AND logical_name=? AND role=? AND blob_id=?
AND game_variant_id IN (SELECT id FROM game_variants WHERE game_id=?)`,
			Args: []any{ref.OwnerID, ref.Key, ref.Qualifier, ref.BlobID, gameID},
		}))
	case gamecontent.RetirementVariantDependency:
		return requireChanged(recordstore.DeleteVariantDependencies(ctx, records.executor, recordstore.Scope{
			Where: `game_variant_id=? AND logical_archive=? AND kind=?
AND game_variant_id IN (SELECT id FROM game_variants WHERE game_id=?)`,
			Args: []any{ref.OwnerID, ref.Key, ref.Qualifier, gameID},
		}))
	default:
		return gamecontent.ErrInvalid
	}
}
