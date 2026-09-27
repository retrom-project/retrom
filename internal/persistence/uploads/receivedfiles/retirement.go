package receivedfiles

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
)

// ReleaseRetired clears transport payload pointers after a domain retires its
// files. Transport identities remain as provenance and cannot block deletion.
// The caller must use the same transaction as the domain retirement.
func ReleaseRetired(ctx context.Context, executor dbapi.Executor, kind, ownerID string, now int64) error {
	const ownedFiles = `(SELECT id FROM stored_files WHERE owner_kind=? AND owner_id=? AND retired_at_ms IS NOT NULL)`
	if _, err := recordstore.UpdateRows(ctx, executor, "import_files", recordstore.Update{
		Set: "blob_id=NULL,released_at_ms=?", Values: []any{now},
		Scope: recordstore.Scope{Where: "blob_id IN " + ownedFiles, Args: []any{kind, ownerID}},
	}); err != nil {
		return fmt.Errorf("release retired received inputs: %w", err)
	}
	if _, err := recordstore.UpdateRows(ctx, executor, "upload_files", recordstore.Update{
		Set: "state='PURGED',final_blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?", Values: []any{now, now},
		Scope: recordstore.Scope{Where: "final_blob_id IN " + ownedFiles, Args: []any{kind, ownerID}},
	}); err != nil {
		return fmt.Errorf("release retired transport payload: %w", err)
	}
	return nil
}
