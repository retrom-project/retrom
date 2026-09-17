package saves

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/model/saves"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/dbexec"
)

type (
	Repository struct {
		database      *sql.DB
		preCommitHook func() error
	}
	records struct{ executor dbexec.Executor }
	writes  struct {
		records
		transaction *sql.Tx
	}
)

// writeScope groups repo-internal record interfaces used inside a single
// checkpoint transaction. These are implementation-private and not part
// of the model port surface.
type writeScope struct {
	launches    launchReader
	idempotency idempotencyRecords
	blobs       blobRecords
	checkpoints checkpointRecords
	gameSaves   gameSaveRecords
}
type launchReader interface {
	LoadLaunch(context.Context, string) (saves.Launch, error)
}
type idempotencyRecords interface {
	Replay(context.Context, saves.ReplayKey) (saves.Replay, bool, error)
	Remember(context.Context, saves.ReplayWrite) error
}
type blobRecords interface {
	Ensure(context.Context, blobstore.Metadata, string, int64) (string, error)
}
type checkpointRecords interface {
	Duration(context.Context, string) (saves.Duration, error)
	CreateSave(context.Context, saves.SaveCreation) error
	ReplacePreview(context.Context, saves.PreviewWrite) error
}
type gameSaveRecords interface {
	Binding(context.Context, string) (saves.GameSaveBinding, bool, error)
	Saved(context.Context, string) (saves.StoredSave, bool, error)
	UpdateSave(context.Context, saves.SaveUpdate) error
	MarkSynced(context.Context, string, string, int64) error
	Bind(context.Context, string, string, int64) error
}

func New(database *sql.DB) *Repository { return &Repository{database: database} }

// WithPreCommitHook sets a test-only hook that runs just before tx.Commit.
func WithPreCommitHook(repo *Repository, hook func() error) {
	repo.preCommitHook = hook
}

func (repository *Repository) LoadLaunch(ctx context.Context, id string) (saves.Launch, error) {
	return (records{executor: repository.database}).LoadLaunch(ctx, id)
}

func (repository *Repository) Restore(ctx context.Context, id string) (saves.Restore, error) {
	return (records{executor: repository.database}).Restore(ctx, id)
}

func (repository *Repository) newWriteScope(tx *sql.Tx) writeScope {
	bound := writes{records: records{executor: tx}, transaction: tx}
	return writeScope{
		launches: bound, idempotency: bound, blobs: bound,
		checkpoints: bound, gameSaves: bound,
	}
}

func (
	repository *Repository) CommitManualCheckpoint(ctx context.Context, cmd saves.ManualCheckpointCommand,
) (saves.ManualResult, bool, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("begin checkpoint transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.newWriteScope(tx)
	result, replayed, err := executeManualCheckpoint(ctx, scope, cmd)
	if err != nil {
		return saves.ManualResult{}, false, err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return saves.ManualResult{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("commit checkpoint transaction: %w", err)
	}
	return result, replayed, nil
}

func (store records) Ensure(
	ctx context.Context,
	metadata blobstore.Metadata,
	mediaType string,
	now int64,
) (string, error) {
	id, err := blobcatalog.EnsureRecord(ctx, store.executor, metadata, mediaType, now)
	if err != nil {
		return "", fmt.Errorf("register checkpoint blob: %w", err)
	}
	return id, nil
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write checkpoint record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count checkpoint records: %w", err)
	}
	if count != 1 {
		return saves.ErrSyncConflict
	}
	return nil
}
