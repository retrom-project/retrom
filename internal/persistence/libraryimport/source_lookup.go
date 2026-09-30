package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type OwnedSources struct{ executor dbapi.Executor }

func BindOwnedSources(executor dbapi.Executor) *OwnedSources {
	return &OwnedSources{executor: executor}
}

func (records *OwnedSources) Lookup(
	ctx context.Context,
	intent libraryservice.SourceCreationIntent,
) (libraryservice.OwnedSourceLookup, bool, error) {
	table, err := sourceOwnerTable(intent.Kind)
	if err != nil {
		return libraryservice.OwnedSourceLookup{}, false, err
	}
	var result libraryservice.OwnedSourceLookup
	var linkedJob, linkedItem, importID string
	err = dbapi.QueryRowContext(ctx, records.executor, `
SELECT COALESCE(source.library_import_job_id,''),COALESCE(source.library_import_item_id,''),
COALESCE(imported.id,''),COALESCE(group_job.id,''),COALESCE(imported.state,''),COALESCE(imported.total_item_count,0),
COALESCE(imported.target_platform_instance_id,''),
COALESCE(json_extract(imported.config_snapshot_json,'$.contentMode'),''),
COALESCE(upload.manifest_digest,'')
FROM `+table+` source
LEFT JOIN server_import_upload_owners owner ON owner.kind=? AND owner.source_item_id=source.id
LEFT JOIN upload_sessions upload ON upload.id=owner.upload_session_id
LEFT JOIN import_jobs imported ON imported.upload_session_id=owner.upload_session_id
LEFT JOIN jobs group_job ON group_job.scope_type='IMPORT_GROUP' AND group_job.scope_id=imported.id
 AND group_job.kind='IMPORT_GROUP'
WHERE source.id=? AND source.import_id=?
AND source.execution_state IN ('COPYING','VALIDATING','REVIEW_PENDING')`,
		string(intent.Kind), intent.ItemID, intent.ImportID).Scan(
		&linkedJob,
		&linkedItem,
		&importID,
		&result.Result.Created.JobID,
		&result.Result.Created.State,
		&result.Result.Created.ItemCount,
		&result.TargetPlatformInstanceID, &result.ContentMode, &result.ManifestDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.OwnedSourceLookup{}, false, libraryservice.ErrInvalid
	}
	if err != nil {
		return libraryservice.OwnedSourceLookup{}, false, fmt.Errorf("lookup bound server source: %w", err)
	}
	if linkedJob == "" && linkedItem == "" && importID == "" {
		return libraryservice.OwnedSourceLookup{}, false, nil
	}
	if linkedJob == "" || linkedItem == "" || linkedJob != importID || result.Result.Created.JobID == "" {
		return libraryservice.OwnedSourceLookup{}, false, libraryservice.ErrVersionConflict
	}
	result.Result.Created.ImportJobID = importID
	result.Result, err = BindSourceResults(records.executor).ReadItem(ctx, result.Result.Created, linkedItem)
	if err != nil {
		return libraryservice.OwnedSourceLookup{}, false, err
	}
	if len(result.Result.Items) != 1 {
		return libraryservice.OwnedSourceLookup{}, false, libraryservice.ErrVersionConflict
	}
	result.PrimaryPaths, err = (sourceOwnership{executor: records.executor}).sourcePaths(ctx, intent.Kind, intent.ItemID)
	if err != nil {
		return libraryservice.OwnedSourceLookup{}, false, err
	}
	return result, true, nil
}
