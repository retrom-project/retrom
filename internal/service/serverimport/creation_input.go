package serverimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/serverimport"

	"github.com/google/uuid"
)

func newCreationPlan(
	request model.CreateRequest,
	actorID string,
	root model.RootSelection,
	items []model.CatalogItem,
	digest string,
	now int64,
) (model.CreationPlan, error) {
	var ids [3]string
	for index := range ids {
		id, err := uuid.NewV7()
		if err != nil {
			return model.CreationPlan{}, fmt.Errorf("create import identity: %w", err)
		}
		ids[index] = id.String()
	}
	input, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "kind": "SERVER_BIOS_IMPORT",
		"scope": map[string]any{"type": "SERVER_IMPORT", "id": ids[0]}, "executionId": ids[2],
		"inputs": map[string]any{
			"serverImportVersion": 1, "rootId": root.ID, "sourceRelativePath": request.SourceRelativePath,
			"rootConfigDigest": root.Digest, "catalogSnapshotDigest": digest, "replaceIfBetter": request.ReplaceIfBetter,
		},
	})
	if err != nil {
		return model.CreationPlan{}, fmt.Errorf("encode import input: %w", err)
	}
	evidence, err := newControlEvidence(actorID, now, []byte(`{"schemaVersion":1}`))
	if err != nil {
		return model.CreationPlan{}, err
	}
	inputDigest := sha256.Sum256(input)
	dedupe := sha256.Sum256([]byte(ids[0]))
	return model.CreationPlan{
		ImportID: ids[0], JobID: ids[1], DedupeKey: hex.EncodeToString(dedupe[:]),
		Request: request, Root: root, Items: items, CatalogDigest: digest,
		Input: input, InputDigest: hex.EncodeToString(inputDigest[:]),
		Payload: []byte(`{"inputExecutionNo":1}`), Audit: []byte(`{"state":"QUEUED"}`), Evidence: evidence,
	}, nil
}
