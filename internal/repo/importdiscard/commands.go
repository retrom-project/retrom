package importdiscard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/model/importdiscard"

	"github.com/google/uuid"
)

func recoverOwnership(ctx context.Context, scope importdiscard.WriteScope, key importdiscard.Key) error {
	ids, err := scope.Ownership.Unlinked(ctx, key)
	if err != nil {
		return failure("recover source ownership", err)
	}
	for _, id := range ids {
		uploadID := uuid.NewSHA1(
			uuid.NameSpaceOID,
			[]byte("retrom:server-source:v1\x00SERVER_"+key.Kind+"_IMPORT:"+id),
		).String()
		importID, err := scope.Ownership.ImportByUpload(ctx, uploadID)
		if err != nil {
			return failure("recover source ownership", err)
		}
		if importID == "" && key.Kind == "PEGASUS" {
			importID, err = legacyOwner(ctx, scope.Ownership, id)
		}
		if err != nil {
			return failure("recover source ownership", err)
		}
		if importID != "" {
			if err := scope.Ownership.Link(ctx, key.Kind, id, importID); err != nil {
				return failure("recover source ownership", err)
			}
		}
	}
	return nil
}

func legacyOwner(ctx context.Context, ownership importdiscard.Ownership, id string) (string, error) {
	candidates, err := ownership.LegacyCandidates(ctx, id)
	if err != nil {
		return "", failure("recover source ownership", err)
	}
	var match string
	for _, candidate := range candidates {
		valid, err := internalEnvelope(candidate)
		if err != nil {
			return "", failure("recover source ownership", err)
		}
		if !valid {
			continue
		}
		if match != "" {
			return "", importdiscard.ErrAmbiguousOwner
		}
		match = candidate.ImportID
	}
	if match == "" {
		return "", nil
	}
	count, err := ownership.OwnerCount(ctx, match)
	if err != nil {
		return "", failure("recover source ownership", err)
	}
	if count != 1 {
		return "", importdiscard.ErrAmbiguousOwner
	}
	return match, nil
}

func internalEnvelope(envelope importdiscard.Envelope) (bool, error) {
	if !envelope.Complete || len(envelope.Files) == 0 {
		return false, nil
	}
	manifest, err := json.Marshal(map[string]any{"schemaVersion": 1, "files": envelope.Files})
	if err != nil {
		return false, fmt.Errorf("encode import envelope: %w", err)
	}
	sum := sha256.Sum256(manifest)
	return envelope.Digest == hex.EncodeToString(sum[:]), nil
}

func discardSourceItems(ctx context.Context, scope importdiscard.WriteScope, key importdiscard.Key, nowMS int64) (bool, error) {
	releases, err := scope.Sources.Releases(ctx, key)
	if err != nil {
		return false, failure("process discarded content", err)
	}
	if releases.Releasing > 0 {
		if releases.Failed > 0 {
			return false, importdiscard.ErrReleaseFailed
		}
		return false, nil
	}
	ids, err := scope.Sources.UnusedUploads(ctx, key)
	if err != nil {
		return false, failure("process discarded content", err)
	}
	for _, id := range ids {
		if err := scope.Sources.DeleteUpload(ctx, id); err != nil {
			return false, failure("process discarded content", err)
		}
	}
	if err := scope.Sources.Complete(ctx, key, nowMS); err != nil {
		return false, failure("process discarded content", err)
	}
	return true, nil
}

func requestDiscard(ctx context.Context, scope importdiscard.WriteScope, cmd importdiscard.RequestDiscardCommand) (importdiscard.Status, error) {
	current, err := discardStatus(ctx, scope.Reader, cmd.Key)
	if err != nil {
		return importdiscard.Status{}, failure("access discard status", err)
	}
	if current.State == "UNAVAILABLE" {
		return importdiscard.Status{}, importdiscard.ErrInvalid
	}
	if current.State != "AVAILABLE" && current.State != "FAILED" {
		return current, nil
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return importdiscard.Status{}, fmt.Errorf("create discard audit identity: %w", err)
	}
	if err := scope.Requests.Request(ctx, importdiscard.Request{
		Key:     cmd.Key,
		UserID:  cmd.UserID,
		AuditID: auditID.String(),
		Now:     cmd.NowMS,
	}); err != nil {
		return importdiscard.Status{}, failure("access discard status", err)
	}
	return importdiscard.Status{Kind: cmd.Key.Kind, ImportID: cmd.Key.ID, State: "REQUESTED"}, nil
}

func discardStatus(ctx context.Context, records importdiscard.Reader, key importdiscard.Key) (importdiscard.Status, error) {
	batch, err := records.Batch(ctx, key)
	if err != nil {
		return importdiscard.Status{}, failure("access discard status", err)
	}
	result := importdiscard.Status{Kind: key.Kind, ImportID: key.ID, State: "UNAVAILABLE"}
	if available(key.Kind, batch) {
		result.State = "AVAILABLE"
	}
	disposition, found, err := records.Disposition(ctx, key)
	if err != nil {
		return importdiscard.Status{}, failure("access discard status", err)
	}
	if found {
		result.State = disposition.State
		result.ErrorCode = disposition.ErrorCode
	}
	return result, nil
}

func available(kind string, batch importdiscard.Batch) bool {
	if !batch.Started || batch.State == "SCANNING" || batch.State == "AWAITING_MAPPING" {
		return false
	}
	if kind == "IMPORT" && retainedImport(batch) {
		return true
	}
	for state, count := range batch.ItemCounts {
		if count > 0 && undecided(kind, state) {
			return true
		}
	}
	return false
}

func retainedImport(batch importdiscard.Batch) bool {
	switch batch.State {
	case "QUEUED", "RUNNING", "CANCEL_REQUESTED":
		return true
	default:
		return batch.Rejected > batch.ResolvedRejected && batch.PayloadState != "RELEASED"
	}
}

func undecided(kind, state string) bool {
	if kind == "IMPORT" {
		return state != "PUBLISHED" && state != "DISCARDED"
	}
	return state != "PUBLISHED" && state != "REVIEW_DISCARDED" && state != "SKIPPED_EXISTING"
}

func failure(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
