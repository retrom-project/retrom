package launch

import (
	"fmt"

	launchmodel "retrom/internal/model/launch"
	application "retrom/internal/service/launch"
)

const (
	rpgProjectFormat         = "RPG_MAKER_PROJECT"
	rpgEasyIndexName         = "__retrom__/index.json"
	rpgMKXPArchiveName       = application.MKXPArchiveName
	rpgMKXPArchivePublicName = application.MKXPArchivePublicName
)

type rpgLockedFile struct{ blobID, logicalName, role string }

func makeRPGContentPlan(files []rpgLockedFile, required string, native bool) (launchContentPlan, error) {
	inputs := make([]launchmodel.PreviewFile, 0, len(files))
	for _, file := range files {
		inputs = append(inputs, launchmodel.PreviewFile{BlobID: file.blobID, LogicalName: file.logicalName, Role: file.role})
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
