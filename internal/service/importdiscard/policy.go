package importdiscard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/importdiscard"
)

type ownershipReader interface {
	LegacyCandidates(context.Context, string) ([]model.Envelope, error)
	OwnerCount(context.Context, string) (int64, error)
}

//nolint:unparam // id is always "source" in tests but the signature matches the repo-layer sibling
func legacyOwner(ctx context.Context, ownership ownershipReader, id string) (string, error) {
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
			return "", model.ErrAmbiguousOwner
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
		return "", model.ErrAmbiguousOwner
	}
	return match, nil
}

func internalEnvelope(envelope model.Envelope) (bool, error) {
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

func available(kind string, batch model.Batch) bool {
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

func retainedImport(batch model.Batch) bool {
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
