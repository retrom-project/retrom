// Package fileownership persists a file's single owner. It does not inspect
// consumers or count references. Domain transactions explicitly transfer or
// retire their files; immutable file IDs are never reused.
package fileownership

import (
	"context"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
)

var ErrOwnerChanged = errors.New("FILE_OWNER_CHANGED")

type Owner struct{ Kind, ID string }

// Transfer is a compare-and-swap of ownership inside the caller's transaction.
// Repeating the same transfer is harmless; a retired file cannot be revived.
func Transfer(ctx context.Context, tx dbapi.Executor, id string, from, to Owner) error {
	if id == "" || from.Kind == "" || (from.ID == "" && from.Kind != "STAGING") || to.Kind == "" || to.ID == "" {
		return ErrOwnerChanged
	}
	result, err := tx.ExecContext(ctx, `UPDATE stored_files SET owner_kind=?,owner_id=?
 WHERE id=? AND retired_at_ms IS NULL AND
 ((owner_kind=? AND owner_id=?) OR (owner_kind=? AND owner_id=?))`,
		to.Kind, to.ID, id, from.Kind, from.ID, to.Kind, to.ID)
	if err != nil {
		return fmt.Errorf("transfer file owner: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check file transfer: %w", err)
	}
	if changed != 1 {
		return ErrOwnerChanged
	}
	return nil
}

func Adopt(ctx context.Context, tx dbapi.Executor, id string, to Owner) error {
	return Transfer(ctx, tx, id, Owner{Kind: "STAGING"}, to)
}

// Retire records an irreversible deletion request for this owner's file.
// Files already handed to another owner are outside this operation's scope.
func Retire(ctx context.Context, tx dbapi.Executor, owner Owner, id string, now int64) error {
	if owner.Kind == "" || owner.ID == "" || id == "" || now < 0 {
		return ErrOwnerChanged
	}
	_, err := tx.ExecContext(ctx, `UPDATE stored_files SET retired_at_ms=COALESCE(retired_at_ms,?)
 WHERE id=? AND owner_kind=? AND owner_id=?`, now, id, owner.Kind, owner.ID)
	if err != nil {
		return fmt.Errorf("retire owned file: %w", err)
	}
	return nil
}

func RetireAll(ctx context.Context, tx dbapi.Executor, owner Owner, now int64) error {
	if owner.Kind == "" || owner.ID == "" || now < 0 {
		return ErrOwnerChanged
	}
	_, err := tx.ExecContext(ctx, `UPDATE stored_files SET retired_at_ms=COALESCE(retired_at_ms,?)
 WHERE owner_kind=? AND owner_id=?`, now, owner.Kind, owner.ID)
	if err != nil {
		return fmt.Errorf("retire owner's files: %w", err)
	}
	return nil
}
