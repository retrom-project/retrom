package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"
)

func TestCreationQueuedDocumentsUseCanonicalDefaultMode(t *testing.T) {
	for _, mode := range []string{"", "STANDARD", "MULTI_DISC"} {
		t.Run(mode, func(t *testing.T) {
			plan := creationPreparedInput()
			request := plan.Request
			request.ContentMode = mode
			target := model.ImportTargetSnapshot{
				SchemaVersion: 1, PlatformInstanceID: plan.Target.ID, PlatformInstanceVersion: plan.Target.Version,
				PlatformID: plan.Target.PlatformID, DefaultCoreID: plan.Target.DefaultCoreID,
				Targets: []model.ImportTargetGuard{TargetImportGuard(plan.Target)},
			}
			current := model.CreationQueuedSnapshot{}
			current.RequestJSON, current.RequestDigest = creationTestDocument(t, model.QueuedImportRequest{
				SchemaVersion: 1, Request: request,
			})
			current.TargetJSON, current.TargetDigest = creationTestDocument(t, target)
			run := creationCommit{plan: plan, options: model.ImportCreationOptions{
				Queued: &model.QueuedImportExecution{Target: target},
			}}
			err := run.checkQueuedDocuments(current)
			if mode == "MULTI_DISC" {
				if !errors.Is(err, model.ErrVersionConflict) {
					t.Fatalf("changed mode accepted: %v", err)
				}
			} else if err != nil {
				t.Fatalf("canonical standard mode %q rejected: %v", mode, err)
			}
		})
	}
}

func creationTestDocument(t *testing.T, value any) (string, string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(digest[:])
}
