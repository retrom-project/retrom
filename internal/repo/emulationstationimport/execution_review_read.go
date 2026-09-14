package emulationstationimport

import (
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

const executionReviewSQL = `SELECT source.id,source.execution_state,source.version,
COALESCE(source.library_import_job_id,''),COALESCE(source.library_import_item_id,''),
source.metadata_json,source.warnings_json,source.retryable,item.import_job_id,item.id,
(SELECT count(*) FROM import_items sibling WHERE sibling.import_job_id=item.import_job_id)
FROM emulationstation_import_items source
JOIN server_import_upload_owners owner ON owner.kind='EMULATIONSTATION' AND owner.source_item_id=source.id
JOIN import_jobs ordinary ON ordinary.upload_session_id=owner.upload_session_id
JOIN import_items item ON item.import_job_id=ordinary.id`

func scanExecutionReview(row dbexec.Scanner) (application.ExecutionReview, error) {
	var value application.ExecutionReview
	var count int
	err := row.Scan(&value.ItemID, &value.State, &value.Version, &value.LibraryJobID, &value.LibraryItemID,
		&value.MetadataJSON, &value.WarningsJSON, &value.Retryable, &value.ReservedJobID, &value.ReservedItemID, &count)
	if err != nil {
		return application.ExecutionReview{}, fmt.Errorf("scan EmulationStation reserved review: %w", err)
	}
	if count != 1 || value.LibraryJobID != "" &&
		(value.LibraryJobID != value.ReservedJobID || value.LibraryItemID != value.ReservedItemID) {
		return application.ExecutionReview{}, application.ErrInvalid
	}
	return value, nil
}
