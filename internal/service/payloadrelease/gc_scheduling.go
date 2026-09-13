package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

const gcPageSize = 200

func (service *GCScheduler) StageInScope(ctx context.Context, scope GCScope, ids []string) error {
	ids = gcUniqueIDs(ids)
	for start := 0; start < len(ids); start += gcPageSize {
		facts, err := scope.Read.Selected(ctx, ids[start:min(start+gcPageSize, len(ids))])
		if err != nil {
			return fmt.Errorf("read selected GC blobs: %w", err)
		}
		if err := service.stage(ctx, scope, facts); err != nil {
			return err
		}
	}
	return nil
}

func (service *GCScheduler) Reconcile(ctx context.Context) error {
	if err := service.cancelProtected(ctx); err != nil {
		return err
	}
	cursor := ""
	for {
		var facts []GCBlob
		err := service.repository.WithGC(ctx, func(scope GCScope) error {
			var err error
			facts, err = scope.Read.Page(ctx, cursor, gcPageSize)
			if err != nil {
				return fmt.Errorf("read GC page: %w", err)
			}
			return service.stage(ctx, scope, facts)
		})
		if err != nil {
			return fmt.Errorf("reconcile GC page: %w", err)
		}
		if len(facts) < gcPageSize {
			return nil
		}
		next := facts[len(facts)-1].ID
		if next <= cursor {
			return ErrGCSnapshotChanged
		}
		cursor = next
	}
}

func (service *GCScheduler) stage(ctx context.Context, scope GCScope, facts []GCBlob) error {
	now := service.now().UnixMilli()
	if now < 0 || now > math.MaxInt64-service.retention.Milliseconds() {
		return ErrGCRetentionInvalid
	}
	var pending []GCQueue
	var selected []GCBlob
	for _, blob := range facts {
		if blob.Protected || blob.HasCandidate {
			continue
		}
		job, err := service.prepareJob(blob, now)
		if err != nil {
			return err
		}
		selected = append(selected, blob)
		pending = append(pending, GCQueue{
			Before: blob, Job: job, AvailableMS: now + service.retention.Milliseconds(),
			EventJSON: `{"schemaVersion":1,"executionNo":1,"attempt":0}`,
		})
	}
	if len(pending) == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop GC scheduling: %w", err)
		}
		return nil
	}
	if err := scope.Write.Fence(ctx, selected); err != nil {
		return fmt.Errorf("fence selected GC blobs: %w", err)
	}
	for _, queue := range pending {
		if err := scope.Write.Queue(ctx, queue); err != nil {
			return fmt.Errorf("queue GC candidate: %w", err)
		}
	}
	return nil
}

func (service *GCScheduler) prepareJob(blob GCBlob, now int64) (ScheduledJob, error) {
	id, err := service.newID()
	if err != nil || id == "" {
		return ScheduledJob{}, fmt.Errorf("GC job identity: %w", gcIdentityError(err))
	}
	encoded, digest, err := service.input(blob)
	if err != nil {
		return ScheduledJob{}, err
	}
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("retrom-job-dedupe-v1\x00BLOB_GC\x00%s\x00%d", blob.ID, now)))
	return ScheduledJob{
		ID: id, Scope: Scope{Type: ScopeBlob, ID: blob.ID}, NowMS: now,
		DedupeKey: hex.EncodeToString(dedupe[:]), InputJSON: encoded, InputDigest: digest,
	}, nil
}

func (service *GCScheduler) input(blob GCBlob) (string, string, error) {
	digest, err := hex.DecodeString(blob.Digest)
	if err != nil || len(digest) != sha256.Size || blob.ID == "" || blob.SizeBytes < 0 {
		return "", "", ErrInputInvalid
	}
	id, err := service.newID()
	if err != nil || id == "" {
		return "", "", fmt.Errorf("GC execution identity: %w", gcIdentityError(err))
	}
	input := Input{
		SchemaVersion: 1, Kind: "BLOB_GC", Scope: Scope{Type: ScopeBlob, ID: blob.ID},
		ExecutionID: id, Inputs: ScopeInputs{SHA256: blob.Digest},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", "", fmt.Errorf("encode GC input: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(hash[:]), nil
}

func gcIdentityError(err error) error {
	if err != nil {
		return fmt.Errorf("%w: %w", ErrScheduleIDInvalid, err)
	}
	return ErrScheduleIDInvalid
}

func gcUniqueIDs(ids []string) []string {
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
