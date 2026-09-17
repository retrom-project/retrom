package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/dependencies"

	"github.com/google/uuid"
)

func ensureBuiltInDATJob(
	ctx context.Context,
	repository model.Repository,
	datID, datSHA, parserVersion string,
	now time.Time,
) (string, error) {
	canonical, err := json.Marshal(map[string]string{"datVersionId": datID, "parserVersion": parserVersion})
	if err != nil {
		return "", fmt.Errorf("encode DAT job identity: %w", err)
	}
	digest := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00DAT_PARSE\x00"), canonical...))
	dedupe := hex.EncodeToString(digest[:])
	jobID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create DAT job ID: %w", err)
	}
	executionID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create DAT execution ID: %w", err)
	}
	id, err := repository.CommitEnsureDATJob(ctx, model.EnsureDATJobCommand{
		DATID: datID, DATSHA: datSHA, ParserVersion: parserVersion,
		DedupeKey: dedupe, JobID: jobID.String(), ExecutionID: executionID.String(),
		NowMS: now.UnixMilli(),
	})
	if err != nil {
		return "", fmt.Errorf("ensure built-in DAT job: %w", err)
	}
	return id, nil
}

func claimBuiltInDATJob(ctx context.Context, repository model.Repository, datID, jobID string, now time.Time) error {
	err := repository.CommitClaimDAT(ctx, model.ClaimDATCommand{
		Claim: model.JobClaim{
			JobID: jobID, DATID: datID, AtMS: now.UnixMilli(),
			DeadlineMS: now.Add(30 * time.Minute).UnixMilli(), LeaseUntilMS: now.Add(time.Minute).UnixMilli(),
			Event: []byte(`{"schemaVersion":1,"executionNo":1,"attempt":1}`),
		},
		MarkDAT: datID,
	})
	if err != nil {
		return fmt.Errorf("claim built-in DAT: %w", err)
	}
	return nil
}

func failBuiltInDAT(ctx context.Context, repository model.Repository, datID, jobID, code string, now time.Time) error {
	event, err := json.Marshal(
		map[string]any{
			"schemaVersion":  1,
			"executionNo":    1,
			"attempt":        1,
			"errorCode":      code,
			"errorRetryable": false,
		},
	)
	if err != nil {
		return fmt.Errorf("encode DAT failure: %w", err)
	}
	if err := repository.CommitFailDAT(ctx, model.FailDATCommand{
		DATID: datID,
		Finish: model.JobFinish{
			JobID: jobID, DATID: datID, State: "FAILED", Code: code,
			AtMS: now.UnixMilli(), Event: event,
		},
	}); err != nil {
		return fmt.Errorf("fail DAT job: %w", err)
	}
	return nil
}
