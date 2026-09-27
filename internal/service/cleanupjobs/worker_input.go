package cleanupjobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/filestore"

	"github.com/google/uuid"
)

func DecodeWork(work Work) (Input, error) {
	var input Input
	if err := json.Unmarshal([]byte(work.InputJSON), &input); err != nil {
		return Input{}, fmt.Errorf("%w: decode payload input: %w", ErrInputInvalid, err)
	}
	digest := sha256.Sum256([]byte(work.InputJSON))
	id, err := uuid.Parse(input.ExecutionID)
	if !work.InputFound || work.InputDigest != hex.EncodeToString(digest[:]) || input.SchemaVersion != 1 ||
		input.Kind != work.Kind || input.Scope != work.Scope || err != nil || id == uuid.Nil {
		return Input{}, ErrInputInvalid
	}
	if err := validateWorkInput(input); err != nil {
		return Input{}, err
	}
	return input, nil
}

func validateWorkInput(input Input) error {
	if input.Kind == "PATH_DELETE" {
		if input.Scope.Type != ScopePath || !filestore.RemovablePath(input.Inputs.RelativePath) {
			return ErrInputInvalid
		}
		return nil
	}
	if !validScheduleScope(input.Scope.Type) || input.Inputs.ScopeVersion < 1 || !validReason(input.Inputs.Reason) {
		return ErrInputInvalid
	}
	return nil
}
