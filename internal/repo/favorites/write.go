package favorites

import (
	"context"
	"fmt"

	"retrom/internal/model/favorites"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type (
	gameRecords        struct{ database dbexec.Executor }
	membershipRecords  struct{ database dbexec.Executor }
	folderRecords      struct{ database dbexec.Executor }
	idempotencyRecords struct{ database dbexec.Executor }
)

func writeScope(database dbexec.Executor) favorites.WriteScope {
	return favorites.WriteScope{
		Games: gameRecords{database}, Memberships: membershipRecords{database},
		Folders: folderRecords{database}, FolderWrites: folderRecords{database},
		Idempotency: idempotencyRecords{database},
	}
}

func (records gameRecords) Remove(ctx context.Context, profileID, gameID string) error {
	if _, err := records.database.ExecContext(ctx,
		"DELETE FROM favorite_folder_games WHERE profile_id=? AND game_id=?", profileID, gameID); err != nil {
		return fmt.Errorf("remove favorite memberships: %w", err)
	}
	if _, err := records.database.ExecContext(ctx,
		"DELETE FROM favorite_games WHERE profile_id=? AND game_id=?", profileID, gameID); err != nil {
		return fmt.Errorf("remove favorite: %w", err)
	}
	return nil
}

func (records folderRecords) Count(ctx context.Context, profileID string) (int, error) {
	var count int
	err := records.database.QueryRowContext(
		ctx, "SELECT count(*) FROM favorite_folders WHERE profile_id=?", profileID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count favorite folders: %w", err)
	}
	return count, nil
}

func (records folderRecords) Create(ctx context.Context, change favorites.FolderWrite) error {
	_, err := records.database.ExecContext(ctx, `
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,1,?,?)`, change.FolderID, change.ProfileID, change.Name, change.NameKey, change.NowMS, change.NowMS)
	if err != nil {
		return fmt.Errorf("create favorite folder: %w", err)
	}
	return nil
}

func (records folderRecords) Rename(ctx context.Context, change favorites.FolderWrite) error {
	result, err := recordstore.UpdateFavoriteFolders(ctx, records.database, recordstore.Update{
		Set: "name=?,name_key=?,version=version+1,updated_at_ms=?",
		Scope: recordstore.Scope{
			Where: "profile_id=? AND id=? AND version=?",
			Args:  []any{change.ProfileID, change.FolderID, change.ExpectedVersion},
		},
		Values: []any{change.Name, change.NameKey, change.NowMS},
	})
	if err != nil {
		return fmt.Errorf("rename favorite folder: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count renamed favorite folders: %w", err)
	}
	if count != 1 {
		return favorites.ErrVersionConflict
	}
	return nil
}

func (records folderRecords) Delete(ctx context.Context, profileID, folderID string, version int64) error {
	if _, err := records.database.ExecContext(ctx,
		"DELETE FROM favorite_folder_games WHERE profile_id=? AND folder_id=?", profileID, folderID); err != nil {
		return fmt.Errorf("delete folder memberships: %w", err)
	}
	result, err := records.database.ExecContext(ctx,
		"DELETE FROM favorite_folders WHERE profile_id=? AND id=? AND version=?", profileID, folderID, version)
	if err != nil {
		return fmt.Errorf("delete favorite folder: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted favorite folders: %w", err)
	}
	if count != 1 {
		return favorites.ErrVersionConflict
	}
	return nil
}
