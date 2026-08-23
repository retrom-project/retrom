package payloadrelease

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
)

func (service *Service) stageAllUnreferenced(ctx context.Context) error {
	if err := service.cancelProtectedCandidates(ctx); err != nil {
		return err
	}
	cursor := ""
	for {
		transaction, err := service.database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("payloadrelease/stage all: %w", err)
		}
		ids, collectErr := collectIDs(ctx, transaction, `
SELECT id FROM blobs WHERE id>? ORDER BY id LIMIT 200
`, cursor)
		if collectErr != nil {
			_ = transaction.Rollback()
			return collectErr
		}
		if err := service.stageCandidates(ctx, transaction, ids); err != nil {
			_ = transaction.Rollback()
			return err
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("payloadrelease/stage all commit: %w", err)
		}
		if len(ids) < 200 {
			return nil
		}
		cursor = ids[len(ids)-1]
	}
}

func (service *Service) cancelProtectedCandidates(ctx context.Context) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/cancel protected: %w", err)
	}
	defer cleanup.Rollback(transaction)
	protected, err := blobregistry.ProtectiveSet(ctx, transaction)
	if err != nil {
		return fmt.Errorf("payloadrelease/cancel protection set: %w", err)
	}
	rows, err := transaction.QueryContext(ctx, `SELECT blob_id,gc_job_id FROM blob_gc_candidates`)
	if err != nil {
		return fmt.Errorf("payloadrelease/list GC candidates: %w", err)
	}
	defer func() { cleanup.Error("close GC candidates", rows.Close()) }()
	type candidate struct{ blobID, jobID string }
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.blobID, &item.jobID); err != nil {
			return fmt.Errorf("payloadrelease/scan GC candidate: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("payloadrelease/iterate GC candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("payloadrelease/close GC candidates: %w", err)
	}
	now := service.now().UnixMilli()
	for _, item := range candidates {
		if _, keep := protected[item.blobID]; !keep {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `DELETE FROM blob_gc_candidates WHERE blob_id=?`, item.blobID); err != nil {
			return fmt.Errorf("payloadrelease/cancel GC candidate: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,error_code=NULL,error_retryable=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='QUEUED'
`, now, now, item.jobID); err != nil {
			return fmt.Errorf("payloadrelease/cancel GC job: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'SUCCEEDED','{"schemaVersion":1,"reason":"REFERENCE_RESTORED"}',?
FROM jobs WHERE id=? AND state='SUCCEEDED' AND finished_at_ms=?
`, now, item.jobID, now); err != nil {
			return fmt.Errorf("payloadrelease/cancel GC event: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/cancel protected commit: %w", err)
	}
	return nil
}

func (service *Service) stageCandidates(ctx context.Context, transaction *sql.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	protected, err := blobregistry.ProtectiveSet(ctx, transaction)
	if err != nil {
		return fmt.Errorf("payloadrelease/protection set: %w", err)
	}
	now := service.now().UnixMilli()
	for _, blobID := range uniqueStrings(ids) {
		if _, keep := protected[blobID]; keep {
			continue
		}
		var digest string
		err := transaction.QueryRowContext(ctx, `SELECT sha256 FROM blobs WHERE id=?`, blobID).Scan(&digest)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("payloadrelease/read blob digest: %w", err)
		}
		var exists int
		if err := transaction.QueryRowContext(
			ctx, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`, blobID,
		).Scan(&exists); err != nil {
			return fmt.Errorf("payloadrelease/read GC candidate: %w", err)
		}
		if exists != 0 {
			continue
		}
		if err := service.insertGCJob(ctx, transaction, blobID, digest, now); err != nil {
			return err
		}
	}
	return nil
}

// StageCandidates registers newly unreferenced payload in the caller's domain
// transaction so the reference removal and GC handoff cannot diverge.
func (service *Service) StageCandidates(ctx context.Context, transaction *sql.Tx, ids []string) error {
	return service.stageCandidates(ctx, transaction, ids)
}

func (service *Service) insertGCJob(ctx context.Context, transaction *sql.Tx, blobID, digest string, now int64) error {
	jobID, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	input := scheduleInput{
		SchemaVersion: 1, Kind: "BLOB_GC", Scope: scheduleScope{Type: ScopeBlob, ID: blobID},
		ExecutionID: executionID.String(), Inputs: scopeInputs{SHA256: digest},
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("retrom-job-dedupe-v1\x00BLOB_GC\x00%s\x00%d", blobID, now)))
	scheduled := now + service.retention.Milliseconds()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'BLOB',?,'BLOB_GC',?,1,'{"inputExecutionNo":1}',0,'QUEUED',0,4,1,?,?,?)
`, jobID.String(), blobID, hex.EncodeToString(dedupe[:]), scheduled, now, now,
	); err != nil {
		return fmt.Errorf("payloadrelease/create GC job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return fmt.Errorf("payloadrelease/create GC input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'BLOB',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)
`, jobID.String(), blobID, now); err != nil {
		return fmt.Errorf("payloadrelease/create GC event: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO blob_gc_candidates(blob_id,gc_job_id,first_unreferenced_at_ms,scheduled_at_ms,attempt_count)
VALUES(?,?,?,?,0)
`, blobID, jobID.String(), now, scheduled); err != nil {
		return fmt.Errorf("payloadrelease/create GC candidate: %w", err)
	}
	return nil
}

func (service *Service) executeBlobGC(ctx context.Context, job claimedJob) error {
	if job.ScopeType != ScopeBlob || len(job.Input.Inputs.SHA256) != 64 {
		return releaseFailure("BLOB_GC_INPUT_INVALID")
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/GC transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	protected, err := blobregistry.ProtectiveSet(ctx, transaction)
	if err != nil {
		return fmt.Errorf("payloadrelease/GC protection: %w", err)
	}
	if _, keep := protected[job.ScopeID]; keep {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM blob_gc_candidates WHERE blob_id=?`, job.ScopeID); err != nil {
			return fmt.Errorf("payloadrelease/GC cancel candidate: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("payloadrelease/GC cancel commit: %w", err)
		}
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx, `DELETE FROM archive_entries WHERE archive_blob_id=?`, job.ScopeID,
	); err != nil {
		return fmt.Errorf("payloadrelease/GC archive: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM blob_gc_candidates WHERE blob_id=?`, job.ScopeID); err != nil {
		return fmt.Errorf("payloadrelease/GC candidate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM blobs WHERE id=?`, job.ScopeID); err != nil {
		return fmt.Errorf("payloadrelease/GC blob: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/GC commit: %w", err)
	}
	path := service.blobs.Path(job.Input.Inputs.SHA256)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return releaseFailure("BLOB_GC_PHYSICAL_DELETE_FAILED")
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
