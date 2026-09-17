package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	service "retrom/internal/model/dependencies"
	"retrom/internal/repo/datindex"

	"retrom/internal/capability/security/authn"
)

var errDATNotReady = errors.New("selected DAT is not ready")

func activateDAT(ctx context.Context, scope service.WriteScope, cmd service.ActivateDATCommand) error {
	state, err := scope.DAT.Activation(ctx, cmd.DATID)
	if err != nil {
		return fmt.Errorf("dependencies/inspect activation: %w", err)
	}
	if state.ParseStatus != "READY" {
		return fmt.Errorf("dependencies/selected DAT: %w", errDATNotReady)
	}
	if state.Active {
		return nil
	}
	if err := scope.DAT.Select(ctx, service.DATSelection{
		ID: cmd.DATID, Target: state.Target, AtMS: cmd.NowMS, AuditID: cmd.AuditID,
		Actor: authn.ActorFromContext(ctx, "release-setup"),
	}); err != nil {
		return fmt.Errorf("dependencies/select DAT: %w", err)
	}
	if err := datindex.SyncRequirements(
		ctx, scope.Requirements, cmd.DATID, time.UnixMilli(cmd.NowMS),
	); err != nil {
		return fmt.Errorf("dependencies/sync requirements: %w", err)
	}
	return nil
}

func buildDATJobCreation(cmd service.EnsureDATJobCommand, version int64) (service.JobCreation, error) {
	input, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "kind": "DAT_PARSE",
		"scope":       map[string]any{"type": "DAT_VERSION", "id": cmd.DATID},
		"executionId": cmd.ExecutionID,
		"inputs": map[string]any{
			"datVersion":    version,
			"datSha256":     cmd.DATSHA,
			"parserVersion": cmd.ParserVersion,
		},
	})
	if err != nil {
		return service.JobCreation{}, fmt.Errorf("dependencies/encode job input: %w", err)
	}
	digest := sha256.Sum256(input)
	return service.JobCreation{
		ID: cmd.JobID, DATID: cmd.DATID, DedupeKey: cmd.DedupeKey,
		Input: input, InputDigest: hex.EncodeToString(digest[:]),
		Payload: []byte(`{"schemaVersion":1,"inputExecutionNo":1}`),
		AtMS:    cmd.NowMS,
		Event:   []byte(`{"schemaVersion":1,"executionNo":1,"attempt":0}`),
	}, nil
}
