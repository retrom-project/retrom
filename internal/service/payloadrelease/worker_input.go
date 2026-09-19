package payloadrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/payloadrelease"

	"github.com/google/uuid"
)

func DecodeWork(work model.Work) (model.Input, error) {
	var input model.Input
	if err := json.Unmarshal([]byte(work.InputJSON), &input); err != nil {
		return model.Input{}, fmt.Errorf("%w: decode payload input: %w", model.ErrInputInvalid, err)
	}
	digest := sha256.Sum256([]byte(work.InputJSON))
	id, err := uuid.Parse(input.ExecutionID)
	if !work.InputFound || work.InputDigest != hex.EncodeToString(digest[:]) || input.SchemaVersion != 1 ||
		input.Kind != work.Kind || input.Scope != work.Scope || err != nil || id == uuid.Nil {
		return model.Input{}, model.ErrInputInvalid
	}
	if err := validateWorkInput(input); err != nil {
		return model.Input{}, err
	}
	return input, nil
}

func validateWorkInput(input model.Input) error {
	if input.Kind == "BLOB_GC" {
		hash, err := hex.DecodeString(input.Inputs.SHA256)
		if input.Scope.Type != model.ScopeBlob || err != nil || len(hash) != sha256.Size {
			return model.ErrInputInvalid
		}
		return nil
	}
	if !model.ValidScheduleScope(input.Scope.Type) || input.Inputs.ScopeVersion < 1 ||
		!model.ValidReason(input.Inputs.Reason) {
		return model.ErrInputInvalid
	}
	return nil
}
