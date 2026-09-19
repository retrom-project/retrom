package importdiscard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/importdiscard"

	"github.com/google/uuid"
)

func (service *Service) recoverSourceLinks(ctx context.Context, key model.Key) error {
	err := service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		ids, err := scope.Ownership.Unlinked(ctx, key)
		if err != nil {
			return failure("recover source ownership", err)
		}
		for _, id := range ids {
			uploadID := uuid.NewSHA1(
				uuid.NameSpaceOID,
				[]byte(
					"retrom:server-source:v1\x00SERVER_"+key.Kind+"_IMPORT:"+id,
				),
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
	})
	return failure("recover source ownership", err)
}

func legacyOwner(ctx context.Context, ownership model.Ownership, id string) (string, error) {
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
