package launch

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"

	"retrom/internal/cleanup"
)

const (
	rpgProjectFormat         = "RPG_MAKER_PROJECT"
	rpgEasyIndexName         = "__retrom__/index.json"
	rpgMKXPArchiveName       = application.MKXPArchiveName
	rpgMKXPArchivePublicName = application.MKXPArchivePublicName
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
ORDER BY 2
`, selection.gameID, selection.variantID)
	if err != nil {
		return launchContentPlan{}, err
	}
	requiredRole, nativeRuntime, err := requiredRPGContent(selection.deliveryProfile)
	if err != nil {
		return launchContentPlan{}, err
	}
	return makeRPGContentPlan(files, requiredRole, nativeRuntime)
}

type rpgLockedFile struct{ blobID, logicalName, role string }

func requiredRPGContent(delivery string) (string, bool, error) {
	role, native, err := application.RPGContentPolicy(delivery)
	if err != nil {
		return "", false, fmt.Errorf("RPG content policy: %w", err)
	}
	return role, native, nil
}

func makeRPGContentPlan(files []rpgLockedFile, required string, native bool) (launchContentPlan, error) {
	inputs := make([]application.PreviewFile, 0, len(files))
	for _, file := range files {
		inputs = append(inputs, application.PreviewFile{BlobID: file.blobID, LogicalName: file.logicalName, Role: file.role})
	}
	prepared, err := application.RPGContentFiles(inputs, required, native)
	if err != nil {
		return launchContentPlan{}, fmt.Errorf("RPG content files: %w", err)
	}
	locked := make([]lockedContentFile, 0, len(prepared))
	for _, file := range prepared {
		locked = append(
			locked,
			lockedContentFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: rpgProjectFormat},
		)
	}
	return launchContentPlan{ContentKind: rpgProjectFormat, Files: locked}, nil
}

func queryLockedContentFiles(
	ctx context.Context,
	queryer dbexec.Executor,
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
