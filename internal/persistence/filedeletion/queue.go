// Package filedeletion persists domain path removal intents in the existing job queue.
package filedeletion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	application "retrom/internal/service/cleanupjobs"

	"github.com/google/uuid"
)

type Records struct{ executor dbapi.Executor }

func Bind(executor dbapi.Executor) application.DeletionScope {
	return application.DeletionScope{Write: Records{executor}}
}

func QueuePath(ctx context.Context, executor dbapi.Executor, path string, now int64) error {
	return (Records{executor}).QueuePath(ctx, path, now)
}

func (records Records) QueuePath(ctx context.Context, path string, now int64) error {
	if !filestore.RemovablePath(path) || now < 0 {
		return filestore.ErrRecordInvalid
	}
	if _, err := records.executor.ExecContext(ctx, `DELETE FROM archive_entries
 WHERE CASE WHEN (archive_file_record IS JSON) THEN ((archive_file_record)::jsonb #>> '{path}') END >= ?
 AND CASE WHEN (archive_file_record IS JSON) THEN ((archive_file_record)::jsonb #>> '{path}') END < ?
`, path+"/", path+"0"); err != nil {
		return fmt.Errorf("remove retired directory archive facts: %w", err)
	}
	digest := sha256.Sum256([]byte("remove-domain-path\x00" + path))
	key := hex.EncodeToString(digest[:])
	var exists bool
	if err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT EXISTS(SELECT 1 FROM jobs WHERE dedupe_key=?)
`, key).Scan(&exists); err != nil {
		return fmt.Errorf("read removal intent: %w", err)
	}
	if exists {
		return nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("allocate removal task: %w", err)
	}
	input := application.Input{
		SchemaVersion: 1, Kind: "PATH_DELETE",
		Scope: application.Scope{Type: application.ScopePath, ID: id.String()}, ExecutionID: id.String(),
		Inputs: application.ScopeInputs{RelativePath: path},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode removal intent: %w", err)
	}
	checksum := sha256.Sum256(encoded)
	if _, err := records.executor.ExecContext(ctx, `INSERT INTO jobs
 (id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,'STORAGE_PATH',?,'PATH_DELETE',?,1,'{"inputExecutionNo":1}',0,'QUEUED',0,4,1,?,?,?)`,
		id.String(), id.String(), key, now, now, now); err != nil {
		return fmt.Errorf("create removal task: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `INSERT INTO job_input_snapshots
 (job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)`,
		id.String(), string(encoded), hex.EncodeToString(checksum[:]), now); err != nil {
		return fmt.Errorf("record removal intent: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `INSERT INTO job_events
 (job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 VALUES(?,'STORAGE_PATH',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)`,
		id.String(), id.String(), now); err != nil {
		return fmt.Errorf("record removal event: %w", err)
	}
	return nil
}

func QueueFile(ctx context.Context, executor dbapi.Executor, value string, now int64) error {
	if value == "" {
		return nil
	}
	directory, err := filestore.CleanupDirectory(value)
	if err != nil {
		return fmt.Errorf("queue file: %w", err)
	}
	if directory == "" {
		return nil
	}
	return QueuePath(ctx, executor, directory, now)
}
