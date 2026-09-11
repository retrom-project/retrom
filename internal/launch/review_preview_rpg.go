package launch

import (
	"context"
	"strings"
)

func (service *Service) reviewPreviewRPGContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	files, err := queryLockedContentFiles(ctx, service.database, `
SELECT file.blob_id,file.logical_name,'PROJECT_FILE'
FROM import_item_source_snapshot_files file
WHERE file.source_snapshot_id=? AND file.role='PROJECT_FILE'
UNION ALL
SELECT file.blob_id,file.logical_name,file.role
FROM import_item_validation_files file
WHERE file.import_item_core_validation_id=?
 AND file.role IN ('RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE')
ORDER BY 2
`, source.SourceSnapshotID, source.ValidationID)
	if err != nil {
		return reviewPreviewContentSet{}, err
	}
	requiredRole, nativeRuntime, err := requiredRPGContent(source.DeliveryProfile)
	if err != nil {
		return reviewPreviewContentSet{}, err
	}
	plan, err := makeRPGContentPlan(files, requiredRole, nativeRuntime)
	if err != nil {
		return reviewPreviewContentSet{}, err
	}
	content := reviewPreviewContentSet{
		Format: plan.ContentKind,
		Files:  make([]reviewPreviewFile, 0, len(plan.Files)-1),
	}
	for _, file := range plan.Files {
		role := "PROJECT_FILE"
		if strings.HasPrefix(file.LogicalName, "__retrom__/") {
			role = "RUNTIME_FILE"
		} else if content.BlobID == "" {
			content.BlobID, content.LogicalName = file.BlobID, file.LogicalName
			continue
		}
		content.Files = append(content.Files, reviewPreviewFile{
			Role: role, LogicalName: file.LogicalName, BlobID: file.BlobID, SortOrder: len(content.Files),
		})
	}
	return content, nil
}
