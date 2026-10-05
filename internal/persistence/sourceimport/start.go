package sourceimport

import (
	"context"
	"fmt"

	payload "retrom/internal/persistence/sourceimport/sourcerelease"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

type Starter struct{ database dbapi.DB }

func NewStarter(database dbapi.DB) *Starter { return &Starter{database: database} }
func (repository *Starter) Inspect(ctx context.Context, id string) (application.StartSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.StartSnapshot{}, fmt.Errorf("begin Source start inspection: %w", err)
	}
	defer dbapi.Rollback(tx)
	result, err := (startRecords{transaction: tx}).Current(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.StartSnapshot{}, fmt.Errorf("finish Source start inspection: %w", err)
	}
	return result, nil
}

func (repository *Starter) WithStart(ctx context.Context, work func(application.StartScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := startRecords{transaction: tx}
		if err := work(application.StartScope{Payload: payload.BindReleases(tx), Read: records, Write: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit Source start: %w", err)
	}
	return nil
}

type startRecords struct{ transaction dbapi.Tx }

func (records startRecords) Current(ctx context.Context, id string) (application.StartSnapshot, error) {
	summary, err := (&Queries{database: records.transaction}).Get(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	result := application.StartSnapshot{Summary: summary}
	err = dbapi.QueryRowContext(ctx, records.transaction, `
SELECT root_config_digest,COALESCE(source_snapshot_digest,''),
NOT EXISTS(SELECT 1 FROM source_import_collections collection CROSS JOIN
 jsonb_array_elements_text((collection.tag_snapshot_json)::jsonb) entry
LEFT JOIN tags tag ON tag.id=((entry.value)::jsonb #>> '{tagId}') AND tag.status='ACTIVE'
WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND tag.id IS NULL),
EXISTS(SELECT 1 FROM source_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))
FROM source_imports WHERE id=?`, id, id, id).
		Scan(&result.RootConfigDigest, &result.SourceSnapshotDigest, &result.TagsValid, &result.OtherActive)
	if err != nil {
		return application.StartSnapshot{}, fmt.Errorf("read Source start readiness: %w", err)
	}
	result.Metadata, err = records.metadata(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	return result, nil
}

func (records startRecords) metadata(ctx context.Context, id string) ([]application.MetadataEvidence, error) {
	rows, err := records.transaction.QueryContext(ctx, `
SELECT relative_path,size_bytes,COALESCE(content_digest,''),source_facts_digest,
parse_state,COALESCE(error_code,'') FROM source_import_metadata_files
WHERE import_id=? ORDER BY relative_path LIMIT ?`, id, application.MaxMetadataFiles+1)
	if err != nil {
		return nil, fmt.Errorf("read Source metadata evidence: %w", err)
	}
	defer func() { cleanup.Error("close Source metadata evidence", rows.Close()) }()
	result := []application.MetadataEvidence{}
	for rows.Next() {
		var value application.MetadataEvidence
		if err := rows.Scan(
			&value.RelativePath,
			&value.SizeBytes,
			&value.ContentDigest,
			&value.FactsDigest,
			&value.ParseState,
			&value.ErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan Source metadata evidence: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Source metadata evidence: %w", err)
	}
	return result, nil
}
