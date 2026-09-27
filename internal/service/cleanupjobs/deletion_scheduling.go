package cleanupjobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const deletionPageSize = 200

func (service *DeletionScheduler) StageInScope(ctx context.Context, scope DeletionScope, ids []string) error {
	ids = deletionUniqueIDs(ids)
	for start := 0; start < len(ids); start += deletionPageSize {
		facts, err := scope.Read.Selected(ctx, ids[start:min(start+deletionPageSize, len(ids))])
		if err != nil {
			return fmt.Errorf("read selected file deletion blobs: %w", err)
		}
		if err := service.stage(ctx, scope, facts); err != nil {
			return err
		}
	}
	return nil
}

func (service *DeletionScheduler) Reconcile(ctx context.Context) error {
	cursor := ""
	for {
		var facts []DeletionFile
		err := service.repository.WithDeletion(ctx, func(scope DeletionScope) error {
			var err error
			facts, err = scope.Read.Page(ctx, cursor, deletionPageSize)
			if err != nil {
				return fmt.Errorf("read file deletion page: %w", err)
			}
			return service.stage(ctx, scope, facts)
		})
		if err != nil {
			return fmt.Errorf("reconcile file deletion page: %w", err)
		}
		if len(facts) < deletionPageSize {
			return nil
		}
		next := facts[len(facts)-1].ID
		if next <= cursor {
			return ErrDeletionSnapshotChanged
		}
		cursor = next
	}
}

func (service *DeletionScheduler) stage(ctx context.Context, scope DeletionScope, facts []DeletionFile) error {
	now := service.now().UnixMilli()
	if now < 0 {
		return ErrInputInvalid
	}
	var pending []DeletionQueue
	var selected []DeletionFile
	for _, blob := range facts {
		if blob.Retained || blob.HasCandidate {
			continue
		}
		job, err := service.prepareJob(blob, now)
		if err != nil {
			return err
		}
		selected = append(selected, blob)
		pending = append(pending, DeletionQueue{
			Before: blob, Job: job, AvailableMS: now,
			EventJSON: `{"schemaVersion":1,"executionNo":1,"attempt":0}`,
		})
	}
	if len(pending) == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop file deletion scheduling: %w", err)
		}
		return nil
	}
	if err := scope.Write.Fence(ctx, selected); err != nil {
		return fmt.Errorf("fence selected file deletion blobs: %w", err)
	}
	for _, queue := range pending {
		if err := scope.Write.Queue(ctx, queue); err != nil {
			return fmt.Errorf("queue file deletion candidate: %w", err)
		}
	}
	return nil
}

func (service *DeletionScheduler) prepareJob(blob DeletionFile, now int64) (ScheduledJob, error) {
	id, err := service.newID()
	if err != nil || id == "" {
		return ScheduledJob{}, fmt.Errorf("file deletion job identity: %w", deletionIdentityError(err))
	}
	encoded, digest, err := service.input(blob)
	if err != nil {
		return ScheduledJob{}, err
	}
	// Candidate ownership provides idempotency; distinct lifetimes can start in the same millisecond.
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00FILE_DELETE\x00" + blob.ID + "\x00" + id))
	return ScheduledJob{
		ID: id, Scope: Scope{Type: ScopeFile, ID: blob.ID}, NowMS: now,
		DedupeKey: hex.EncodeToString(dedupe[:]), InputJSON: encoded, InputDigest: digest,
	}, nil
}

func (service *DeletionScheduler) input(blob DeletionFile) (string, string, error) {
	digest, err := hex.DecodeString(blob.Digest)
	if err != nil || len(digest) != sha256.Size || blob.ID == "" || blob.SizeBytes < 0 {
		return "", "", ErrInputInvalid
	}
	id, err := service.newID()
	if err != nil || id == "" {
		return "", "", fmt.Errorf("file deletion execution identity: %w", deletionIdentityError(err))
	}
	input := Input{
		SchemaVersion: 1, Kind: "FILE_DELETE", Scope: Scope{Type: ScopeFile, ID: blob.ID},
		ExecutionID: id, Inputs: ScopeInputs{SHA256: blob.Digest},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", "", fmt.Errorf("encode file deletion input: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(hash[:]), nil
}

func deletionIdentityError(err error) error {
	if err != nil {
		return fmt.Errorf("%w: %w", ErrScheduleIDInvalid, err)
	}
	return ErrScheduleIDInvalid
}

func deletionUniqueIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}
