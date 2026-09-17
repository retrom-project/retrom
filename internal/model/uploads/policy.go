package uploads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/google/uuid"
)

// DecodeFinalization validates and decodes a finalization job's input.
func DecodeFinalization(job Job) (FinalizationInput, error) {
	var input FinalizationInput
	digest := sha256.Sum256([]byte(job.Input))
	if hex.EncodeToString(digest[:]) != job.InputDigest {
		return input, ErrInputInvalid
	}
	if err := json.Unmarshal([]byte(job.Input), &input); err != nil {
		return input, errors.Join(ErrInputInvalid, err)
	}
	id, err := uuid.Parse(input.ExecutionID)
	if err != nil {
		return input, errors.Join(ErrInputInvalid, err)
	}
	if id.Version() != 7 || input.SchemaVersion != 1 || input.Kind != "UPLOAD_FINALIZE" || job.Kind != input.Kind ||
		job.Scope != "UPLOAD_SESSION" || input.Scope.Type != job.Scope || input.Scope.ID != job.ScopeID ||
		input.Inputs.FinalizationNo < 1 {
		return input, ErrInputInvalid
	}
	return input, nil
}

// ValidateFinalizationFiles checks that manifest files match the input.
func ValidateFinalizationFiles(input FinalizationInput, files []FrozenFile) error {
	byID := make(map[string]FrozenFile, len(input.Inputs.Files))
	for _, file := range input.Inputs.Files {
		if _, exists := byID[file.ID]; exists {
			return ErrInputInvalid
		}
		byID[file.ID] = file
	}
	for _, file := range files {
		expected, exists := byID[file.ID]
		if !exists || !reflect.DeepEqual(expected, file) {
			return ErrInputInvalid
		}
	}
	return nil
}
