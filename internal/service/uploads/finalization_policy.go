package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
)

var (
	ErrExecutionLost     = errors.New("UPLOAD_EXECUTION_LOST")
	ErrWorkerClosed      = errors.New("UPLOAD_WORKER_CLOSED")
	ErrInputInvalid      = errors.New("UPLOAD_INPUT_INVALID")
	ErrAttemptsExhausted = errors.New("UPLOAD_ATTEMPTS_EXHAUSTED")
)

const finalizationTimeout = 10 * time.Minute

func decodeFinalization(job Job) (FinalizationInput, error) {
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

func validateFinalizationFiles(input FinalizationInput, files []FrozenFile) error {
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

func executionOwned(job Job, run Run) bool {
	return job.ExecutionNo == run.ExecutionNo && job.WorkerID == run.WorkerID && job.Attempt == run.Attempt
}

func executionActive(job Job, run Run, now int64) error {
	if !executionOwned(job, run) || job.State != "RUNNING" {
		return ErrExecutionLost
	}
	if job.Deadline <= now {
		return context.DeadlineExceeded
	}
	if job.Lease <= now {
		return ErrExecutionLost
	}
	return nil
}

func finalizationFailure(cause error, deadline, now int64) (string, bool) {
	switch {
	case errors.Is(cause, errPartMissing):
		return "UPLOAD_PART_MISSING", false
	case errors.Is(cause, errPartCorrupt):
		return "UPLOAD_PART_CORRUPT", false
	case errors.Is(cause, ErrInputInvalid):
		return "UPLOAD_INPUT_INVALID", false
	case errors.Is(cause, ErrAttemptsExhausted):
		return "UPLOAD_ATTEMPTS_EXHAUSTED", true
	case deadline > 0 && now >= deadline:
		return "UPLOAD_FINALIZE_TIMEOUT", true
	default:
		return "UPLOAD_FINALIZE_IO", true
	}
}

func finalizationEvent(run Run, code string, cause error) []byte {
	payload := map[string]any{
		"schemaVersion": 1, "executionNo": run.ExecutionNo, "attempt": run.Attempt, "errorCode": code,
	}
	var part *BrokenPart
	if errors.As(cause, &part) {
		payload["failedPart"] = part
	}
	result, _ := json.Marshal(payload)
	return result
}
