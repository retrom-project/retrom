package gamecontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const replacementExecutionTimeout = 6 * time.Hour

// Run processes a persisted execution; queue recovery can invoke the same entry point.
func (service *Service) Run(parent context.Context, jobID string, executionNo int64) error {
	ctx, cancel := context.WithTimeout(parent, replacementExecutionTimeout)
	defer cancel()
	snapshot, inputDigest, err := service.input(ctx, jobID, executionNo)
	if err != nil {
		return err
	}
	workerID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create replacement worker identity: %w", err)
	}
	now := service.now().UnixMilli()
	claim := Claim{
		GameID:      snapshot.GameID,
		JobID:       jobID,
		WorkerID:    workerID.String(),
		InputDigest: inputDigest,
		ExecutionNo: executionNo,
		Now:         now,
		Deadline:    now + replacementExecutionTimeout.Milliseconds(),
	}
	var claimed bool
	err = service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		var err error
		claimed, err = scope.Leases.Claim(ctx, claim)
		if err != nil {
			return fmt.Errorf("claim replacement execution: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("start replacement execution: %w", err)
	}
	if !claimed {
		return nil
	}
	stopped := make(chan struct{})
	go service.heartbeat(ctx, cancel, claim, stopped)
	defer func() { cancel(); <-stopped }()
	prepared, err := service.prepare(ctx, snapshot)
	if err == nil {
		err = service.publish(ctx, claim, snapshot, prepared)
	}
	if err == nil {
		return nil
	}
	return service.settleFailure(parent, claim, snapshot, err)
}

func (service *Service) input(ctx context.Context, id string, execution int64) (JobSnapshot, string, error) {
	var stored StoredInput
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		var err error
		stored, err = scope.Inputs.Input(ctx, id, execution)
		if err != nil {
			return fmt.Errorf("read replacement input: %w", err)
		}
		return nil
	})
	if err != nil {
		return JobSnapshot{}, "", fmt.Errorf("load replacement execution: %w", err)
	}
	digest := sha256.Sum256(stored.Contents)
	if hex.EncodeToString(digest[:]) != stored.Digest {
		return JobSnapshot{}, "", ErrInvalid
	}
	var envelope inputEnvelope
	if err := json.Unmarshal(stored.Contents, &envelope); err != nil {
		return JobSnapshot{}, "", fmt.Errorf("%w: decode input: %w", ErrInvalid, err)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "GAME_CONTENT_REPLACE" || envelope.Scope.Type != "GAME" ||
		envelope.Scope.ID != envelope.Inputs.GameID {
		return JobSnapshot{}, "", ErrInvalid
	}
	if _, err := uuid.Parse(envelope.ExecutionID); err != nil {
		return JobSnapshot{}, "", ErrInvalid
	}
	// Retry changes the envelope execution identity while preserving frozen business inputs.
	envelope.Inputs.ExecutionID = envelope.ExecutionID
	return envelope.Inputs, stored.Digest, nil
}

func (service *Service) heartbeat(
	ctx context.Context,
	cancel context.CancelFunc,
	claim Claim,
	stopped chan<- struct{},
) {
	defer close(stopped)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := false
			err := service.repository.CommitWrite(ctx, func(scope WriteScope) error {
				var err error
				current, err = scope.Leases.Refresh(ctx, claim, service.now().UnixMilli())
				if err != nil {
					return fmt.Errorf("refresh replacement lease: %w", err)
				}
				return nil
			})
			if err != nil || !current {
				cancel()
				return
			}
		}
	}
}

func (service *Service) prepare(ctx context.Context, snapshot JobSnapshot) (PreparedReplacement, error) {
	var files []UploadedFile
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		var err error
		files, err = scope.Content.Files(ctx, snapshot.UploadSessionID)
		if err != nil {
			return fmt.Errorf("read replacement files: %w", err)
		}
		return nil
	})
	if err != nil {
		return PreparedReplacement{}, fmt.Errorf("load replacement content: %w", err)
	}
	return service.prepareReplacement(ctx, snapshot, files)
}

func failureOutcome(claim Claim, snapshot JobSnapshot, err error, now int64) Outcome {
	outcome := Outcome{
		Claim:     claim,
		GameID:    snapshot.GameID,
		Code:      "GAME_CONTENT_INPUT_UNAVAILABLE",
		Retryable: true,
		Now:       now,
	}
	var validation *replacementValidationError
	if errors.As(err, &validation) {
		outcome.Code = validation.code
		outcome.Retryable = false
	}
	return outcome
}

func (service *Service) settleFailure(parent context.Context, claim Claim, snapshot JobSnapshot, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	outcome := failureOutcome(claim, snapshot, cause, service.now().UnixMilli())
	changed := false
	err := service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		state, err := scope.Leases.State(ctx, claim)
		if err != nil {
			return fmt.Errorf("read failed replacement ownership: %w", err)
		}
		if state != "RUNNING" && state != "CANCEL_REQUESTED" {
			return nil
		}
		if state == "CANCEL_REQUESTED" {
			outcome.Cancelled = true
			outcome.Retryable = false
		}
		changed, err = scope.Jobs.Fail(ctx, outcome)
		if err != nil {
			return fmt.Errorf("finish failed replacement: %w", err)
		}
		if changed && !outcome.Retryable {
			if err := releaseReplacementUpload(ctx, scope.Retirements, claim.JobID, outcome.Now); err != nil {
				return fmt.Errorf("release terminal replacement upload: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("settle replacement execution: %w", err))
	}
	if changed && !outcome.Retryable && service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
	return nil
}
