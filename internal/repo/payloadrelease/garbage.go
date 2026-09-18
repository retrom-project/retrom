package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
)

type Garbage struct{ database *sql.DB }

func NewGarbage(database *sql.DB) *Garbage { return &Garbage{database: database} }

func (repository *Garbage) LoadGarbageFacts(
	ctx context.Context, id, digest string,
) (application.GarbageFacts, error) {
	return garbageRecords{executor: repository.database}.Facts(ctx, id, digest)
}

func (repository *Garbage) LoadGarbageWork(
	ctx context.Context, id string,
) (application.Work, bool, error) {
	return workerRecords{executor: repository.database}.Current(ctx, id)
}

func (repository *Garbage) CommitGarbage(
	ctx context.Context,
	cmd application.GarbageCommand,
	authority application.EffectAuthority,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin garbage transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := BindWorker(tx)
	if err := authority.CheckInScope(
		ctx, scope, cmd.WorkFence,
	); err != nil {
		return fmt.Errorf("fence garbage authority: %w", err)
	}
	records := garbageRecords{executor: tx}
	if cmd.Remove {
		if err := records.Remove(ctx, cmd.Facts); err != nil {
			return err
		}
	} else if cmd.Cancel {
		if err := records.Cancel(ctx, cmd.Facts); err != nil {
			return err
		}
	}
	if err := authority.CheckInScope(
		ctx, scope, cmd.WorkFence,
	); err != nil {
		return fmt.Errorf("confirm garbage authority: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit garbage transaction: %w", err)
	}
	return nil
}

type garbageRecords struct{ executor dbexec.Executor }

func (records garbageRecords) Facts(ctx context.Context, id, digest string) (application.GarbageFacts, error) {
	var facts application.GarbageFacts
	blobs, err := gcRecords(records).Selected(ctx, []string{id})
	if err != nil {
		return facts, fmt.Errorf("read garbage Blob: %w", err)
	}
	if len(blobs) == 1 {
		facts.Found = true
		facts.Blob = blobs[0]
		if err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM archive_entries WHERE archive_blob_id=?`, id).
			Scan(&facts.ArchiveEntries); err != nil {
			return application.GarbageFacts{}, fmt.Errorf("read garbage archive size: %w", err)
		}
	}
	err = records.executor.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blobs WHERE sha256=? AND id<>?)`, digest, id).
		Scan(&facts.OtherDigestOwner)
	if err != nil {
		return application.GarbageFacts{}, fmt.Errorf("read replacement garbage digest owner: %w", err)
	}
	return facts, nil
}

func (records garbageRecords) Cancel(ctx context.Context, facts application.GarbageFacts) error {
	if err := gcRecords(records).Fence(ctx, []application.GCBlob{facts.Blob}); err != nil {
		return fmt.Errorf("fence protected garbage: %w", err)
	}
	return records.cancelCandidate(ctx, facts.Blob)
}

func (records garbageRecords) cancelCandidate(ctx context.Context, blob application.GCBlob) error {
	if !blob.HasCandidate {
		return nil
	}
	result, err := records.executor.ExecContext(ctx, `DELETE FROM blob_gc_candidates`+gcCandidateFence,
		gcCandidateArguments(blob)...)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("remove garbage candidate: %w", err)
	}
	return nil
}

func (records garbageRecords) Remove(ctx context.Context, facts application.GarbageFacts) error {
	if err := gcRecords(records).Fence(ctx, []application.GCBlob{facts.Blob}); err != nil {
		return fmt.Errorf("fence garbage before deletion: %w", err)
	}
	result, err := records.executor.ExecContext(ctx, `DELETE FROM archive_entries WHERE archive_blob_id=?`, facts.Blob.ID)
	if err := gcWriteCount(result, err, facts.ArchiveEntries); err != nil {
		return fmt.Errorf("remove garbage archive index: %w", err)
	}
	if err := records.cancelCandidate(ctx, facts.Blob); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `DELETE FROM blobs WHERE id=? AND sha256=? AND size_bytes=?`,
		facts.Blob.ID, facts.Blob.Digest, facts.Blob.SizeBytes)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("remove garbage Blob: %w", err)
	}
	return nil
}
