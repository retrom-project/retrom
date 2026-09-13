package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	library "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/maintenance"
)

type reviewRecords struct{ executor dbexec.Executor }

func (writes writes) Reviews() application.RestoredReviewScope {
	return application.RestoredReviewScope{
		Records:  reviewRecords{writes.transaction},
		Metadata: library.BindMetadata(writes.transaction),
	}
}

func (records reviewRecords) Pending(
	ctx context.Context, query application.RestoredReviewQuery,
) ([]application.RestoredReview, error) {
	statement, err := restoredReviewSQL(query.Kind)
	if err != nil {
		return nil, err
	}
	rows, err := records.executor.QueryContext(ctx, statement+` AND source.id>? ORDER BY source.id LIMIT ?`,
		query.AfterID, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("query restored review handoffs: %w", err)
	}
	defer func() { cleanup.Error("close restored review handoffs", rows.Close()) }()
	result := []application.RestoredReview{}
	for rows.Next() {
		value, err := scanRestoredReview(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate restored review handoffs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close restored review handoffs: %w", err)
	}
	return result, nil
}

func (records reviewRecords) current(
	ctx context.Context, kind, id string,
) (application.RestoredReview, error) {
	statement, err := restoredReviewSQL(kind)
	if err != nil {
		return application.RestoredReview{}, err
	}
	value, err := scanRestoredReview(records.executor.QueryRowContext(ctx, statement+` AND source.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return application.RestoredReview{}, errors.Join(application.ErrInvalidBundle, err)
	}
	return value, err
}

func scanRestoredReview(scanner dbexec.Scanner) (application.RestoredReview, error) {
	var value application.RestoredReview
	err := scanner.Scan(&value.Kind, &value.ItemID, &value.ImportID, &value.JobID, &value.State,
		&value.ImportState, &value.JobState, &value.Version, &value.ImportVersion, &value.JobVersion,
		&value.ExecutionNo, &value.LibraryJobID, &value.LibraryItemID, &value.ReservedJobID,
		&value.ReservedItemID, &value.UploadID, &value.OwnerUpload, &value.OwnerKind, &value.OwnerItemID,
		&value.OrdinaryItemCount,
		&value.OrdinaryVersion, &value.MetadataJSON, &value.WarningsJSON, &value.RootID,
		&value.RootDigest, &value.RelativePath, &value.CreatorID, &value.ReleaseYearMax, &value.Retryable)
	if err != nil {
		return application.RestoredReview{}, fmt.Errorf("read restored review handoff: %w", err)
	}
	return value, nil
}

func restoredReviewSQL(kind string) (string, error) {
	var prefix, year, owner, joins, states string
	switch kind {
	case "PEGASUS":
		prefix, year, owner = "pegasus", "0", "COALESCE(owner.upload_session_id,'')"
		joins = `JOIN import_jobs ordinary ON ordinary.id=source.library_import_job_id
JOIN import_items item ON item.id=source.library_import_item_id AND item.import_job_id=ordinary.id
LEFT JOIN server_import_upload_owners owner ON owner.upload_session_id=ordinary.upload_session_id`
		states = `source.execution_state IN ('PENDING','COPYING','VALIDATING')`
	case "EMULATIONSTATION":
		prefix, year, owner = "emulationstation", "plan.release_year_max", "owner.upload_session_id"
		joins = `JOIN server_import_upload_owners owner
ON owner.kind='EMULATIONSTATION' AND owner.source_item_id=source.id
JOIN import_jobs ordinary ON ordinary.upload_session_id=owner.upload_session_id
JOIN import_items item ON item.import_job_id=ordinary.id AND item.review_handoff_kind='EMULATIONSTATION'`
		states = `(source.execution_state IN ('PENDING','COPYING','VALIDATING') OR
source.retryable=1 AND source.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED'))`
	default:
		return "", application.ErrInvalidBundle
	}
	return `SELECT '` + kind + `',source.id,plan.id,job.id,source.execution_state,plan.state,job.state,
source.version,plan.version,job.version,job.execution_no,
COALESCE(source.library_import_job_id,''),COALESCE(source.library_import_item_id,''),
ordinary.id,item.id,ordinary.upload_session_id,` + owner + `,
COALESCE(owner.kind,''),COALESCE(owner.source_item_id,''),
(SELECT count(*) FROM import_items sibling WHERE sibling.import_job_id=ordinary.id),item.version,
source.metadata_json,source.warnings_json,plan.root_id,plan.root_config_digest,plan.source_relative_path,
plan.created_by_user_id,` + year + `,source.retryable
FROM ` + prefix + `_import_items source JOIN ` + prefix + `_imports plan ON plan.id=source.import_id
JOIN jobs job ON job.id=plan.import_job_id AND job.scope_type='` + kind + `_IMPORT'
AND job.scope_id=plan.id AND job.kind='SERVER_` + kind + `_IMPORT'
` + joins + ` WHERE plan.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
AND job.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') AND item.state='REVIEW_PENDING'
AND ` + states, nil
}
