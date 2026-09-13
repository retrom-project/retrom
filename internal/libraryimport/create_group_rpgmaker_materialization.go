package libraryimport

import (
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
)

func (run *creationRun) persistPreparedValidationArtifacts(record *groupRecord) error {
	writer := repository.BindPreparedArtifacts(run.transaction)
	for index := range record.group.ValidationFiles {
		file := &record.group.ValidationFiles[index]
		if file.Artifact == nil {
			continue
		}
		blobID, err := writer.Register(run.ctx, *file.Artifact, run.now)
		if err != nil {
			return fmt.Errorf("persist prepared validation artifact: %w", err)
		}
		file.BlobID = blobID
	}
	return nil
}
