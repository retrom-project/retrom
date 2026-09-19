package importdiscard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	model "retrom/internal/model/importdiscard"
)

type memoryOwnership struct {
	model.Ownership

	candidates []model.Envelope
	count      int64
	fail       error
}

func (ownership memoryOwnership) LegacyCandidates(context.Context, string) ([]model.Envelope, error) {
	return ownership.candidates, ownership.fail
}

func (ownership memoryOwnership) OwnerCount(context.Context, string) (int64, error) {
	return ownership.count, ownership.fail
}

func TestLegacyEnvelopeRequiresCompleteCanonicalContentAndUniqueOwner(t *testing.T) {
	// A literal canonical manifest also locks the cross-module JSON representation.
	sum := sha256.Sum256([]byte(`{"files":[{"RelativePath":"game.nes","BlobID":"blob","SizeBytes":8}],"schemaVersion":1}`))
	envelope := model.Envelope{
		ImportID: "import",
		Complete: true,
		Digest: hex.EncodeToString(
			sum[:],
		),
		Files: []model.EnvelopeFile{
			{
				RelativePath: "game.nes",
				BlobID:       "blob",
				SizeBytes:    8,
			},
		},
	}
	ownership := memoryOwnership{candidates: []model.Envelope{envelope}, count: 1}
	if id, err := legacyOwner(t.Context(), ownership, "source"); err != nil || id != "import" {
		t.Fatalf("valid envelope: %q %v", id, err)
	}
	ownership.count = 2
	if _, err := legacyOwner(t.Context(), ownership, "source"); !errors.Is(err, model.ErrAmbiguousOwner) {
		t.Fatalf("ambiguous owner: %v", err)
	}
	ownership.count = 1
	ownership.candidates = append(ownership.candidates, envelope)
	if _, err := legacyOwner(t.Context(), ownership, "source"); !errors.Is(err, model.ErrAmbiguousOwner) {
		t.Fatalf("ambiguous envelopes: %v", err)
	}
	ownership.candidates = []model.Envelope{envelope}
	ownership.candidates[0].Complete = false
	if id, err := legacyOwner(t.Context(), ownership, "source"); err != nil || id != "" {
		t.Fatalf("incomplete envelope: %q %v", id, err)
	}
	ownership.candidates[0].Complete = true
	ownership.candidates[0].Digest = "browser-upload-digest"
	if id, err := legacyOwner(t.Context(), ownership, "source"); err != nil || id != "" {
		t.Fatalf("non-internal envelope: %q %v", id, err)
	}
	ownership.fail = context.Canceled
	if _, err := legacyOwner(t.Context(), ownership, "source"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cause: %v", err)
	}
}
