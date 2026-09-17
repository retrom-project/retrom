package serverimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/serverimport"

	"github.com/google/uuid"
)

func newManualRetry(before model.ControlSnapshot, actorID string, now int64) (model.ManualRetry, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.ManualRetry{}, fmt.Errorf("create import retry identity: %w", err)
	}
	summary := before.Summary
	execution := before.Execution + 1
	input, err := json.Marshal(
		map[string]any{
			"schemaVersion": 1,
			"kind":          "SERVER_BIOS_IMPORT",
			"scope": map[string]any{
				"type": "SERVER_IMPORT",
				"id":   summary.ID,
			},
			"executionId": id.String(),
			"inputs": map[string]any{
				"serverImportVersion":   summary.Version,
				"rootId":                summary.Root.ID,
				"sourceRelativePath":    summary.SourceRelativePath,
				"rootConfigDigest":      before.RootDigest,
				"catalogSnapshotDigest": before.CatalogDigest,
				"replaceIfBetter":       summary.ReplaceIfBetter,
			},
		},
	)
	if err != nil {
		return model.ManualRetry{}, fmt.Errorf("encode import retry input: %w", err)
	}
	payload, err := json.Marshal(map[string]any{"inputExecutionNo": execution})
	if err != nil {
		return model.ManualRetry{}, fmt.Errorf("encode import retry payload: %w", err)
	}
	event, err := json.Marshal(map[string]any{"schemaVersion": 1, "executionNo": execution})
	if err != nil {
		return model.ManualRetry{}, fmt.Errorf("encode import retry event: %w", err)
	}
	evidence, err := newControlEvidence(actorID, now, event)
	if err != nil {
		return model.ManualRetry{}, err
	}
	digest := sha256.Sum256(input)
	return model.ManualRetry{
		Before:    before,
		Execution: execution,
		Input:     input,
		InputDigest: hex.EncodeToString(
			digest[:],
		),
		Payload:  payload,
		Evidence: evidence,
	}, nil
}
