package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
)

type Starter struct{ database *sql.DB }

func NewStarter(database *sql.DB) *Starter { return &Starter{database: database} }
func (repository *Starter) Inspect(ctx context.Context, id string) (application.StartSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.StartSnapshot{}, fmt.Errorf("begin Pegasus start inspection: %w", err)
	}
	defer dbexec.Rollback(tx)
	result, err := (startRecords{transaction: tx}).Current(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.StartSnapshot{}, fmt.Errorf("finish Pegasus start inspection: %w", err)
	}
	return result, nil
}

func (repository *Starter) CommitStart(ctx context.Context, plan application.StartPlan) (application.Summary, bool, error) {
	var result application.Summary
	var queued bool
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := startRecords{transaction: executor}
		current, err := records.Current(ctx, plan.Before.Summary.ID)
		if err != nil {
			return fmt.Errorf("reread Pegasus start: %w", err)
		}
		if current.Summary.State != "AWAITING_MAPPING" {
			result = current.Summary
			return nil
		}
		if current.Summary.Version != plan.Before.Summary.Version {
			return application.ErrVersionConflict
		}
		if plan.NowMS >= current.Summary.ExpiresAtMS {
			return application.ErrExpired
		}
		mapped := current.Summary.Counts.MappedCollections + current.Summary.Counts.SkippedCollections
		if mapped != current.Summary.Counts.Collections || !current.TagsValid {
			return application.ErrMapping
		}
		if current.Summary.Counts.MappedCollections == 0 {
			return application.ErrNoSelection
		}
		if current.OtherActive {
			return application.ErrActive
		}
		if current.Summary.ImportJobID != nil {
			return application.ErrMapping
		}
		if current.RootConfigDigest != plan.Before.RootConfigDigest ||
			current.SourceSnapshotDigest != plan.Before.SourceSnapshotDigest {
			return application.ErrSourceChanged
		}
		plan.Before = current
		if err := records.Queue(ctx, plan); err != nil {
			return fmt.Errorf("queue Pegasus start: %w", err)
		}
		if err := scheduleTerminalPayloads(ctx, executor, plan.Before.Summary.ID, plan.NowMS); err != nil {
			return err
		}
		after, err := records.Current(ctx, current.Summary.ID)
		if err != nil {
			return fmt.Errorf("read queued Pegasus import: %w", err)
		}
		result, queued = after.Summary, true
		return nil
	})
	if err != nil {
		return application.Summary{}, false, err
	}
	return result, queued, nil
}

type startRecords struct{ transaction dbexec.Executor }

func (records startRecords) Current(ctx context.Context, id string) (application.StartSnapshot, error) {
	summary, err := (&Queries{database: records.transaction}).Get(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	result := application.StartSnapshot{Summary: summary}
	err = records.transaction.QueryRowContext(ctx, `
SELECT root_config_digest,COALESCE(source_snapshot_digest,''),
NOT EXISTS(SELECT 1 FROM pegasus_import_collections collection JOIN json_each(collection.tag_snapshot_json) entry
LEFT JOIN tags tag ON tag.id=json_extract(entry.value,'$.tagId') AND tag.status='ACTIVE'
WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND tag.id IS NULL),
EXISTS(SELECT 1 FROM pegasus_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))
FROM pegasus_imports WHERE id=?`, id, id, id).
		Scan(&result.RootConfigDigest, &result.SourceSnapshotDigest, &result.TagsValid, &result.OtherActive)
	if err != nil {
		return application.StartSnapshot{}, fmt.Errorf("read Pegasus start readiness: %w", err)
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
parse_state,COALESCE(error_code,'') FROM pegasus_import_metadata_files
WHERE import_id=? ORDER BY relative_path LIMIT ?`, id, application.MaxMetadataFiles+1)
	if err != nil {
		return nil, fmt.Errorf("read Pegasus metadata evidence: %w", err)
	}
	defer func() { cleanup.Error("close Pegasus metadata evidence", rows.Close()) }()
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
			return nil, fmt.Errorf("scan Pegasus metadata evidence: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Pegasus metadata evidence: %w", err)
	}
	return result, nil
}
