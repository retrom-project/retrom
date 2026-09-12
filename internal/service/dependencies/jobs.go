package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func ensureBuiltInDATJob(
	ctx context.Context,
	repository Repository,
	datID, datSHA, parserVersion string,
	now time.Time,
) (string, error) {
	canonical, err := json.Marshal(map[string]string{"datVersionId": datID, "parserVersion": parserVersion})
	if err != nil {
		return "", fmt.Errorf("encode DAT job identity: %w", err)
	}
	digest := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00DAT_PARSE\x00"), canonical...))
	dedupe := hex.EncodeToString(digest[:])
	var id string
	err = repository.WithWrite(ctx, func(scope WriteScope) error {
		job, found, err := scope.Jobs.Find(ctx, dedupe)
		if err != nil {
			return fmt.Errorf("find built-in DAT job: %w", err)
		}
		if found {
			if err := recoverDATJob(ctx, scope.Jobs, job, now.UnixMilli()); err != nil {
				return err
			}
			id = job.ID
			return nil
		}
		creation, err := prepareDATJob(ctx, scope.Catalog, datID, datSHA, parserVersion, dedupe, now)
		if err != nil {
			return err
		}
		if err := scope.Jobs.Create(ctx, creation); err != nil {
			return fmt.Errorf("create DAT job: %w", err)
		}
		id = creation.ID
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("ensure built-in DAT job: %w", err)
	}
	return id, nil
}

func prepareDATJob(
	ctx context.Context,
	catalog CatalogRecords,
	datID, datSHA, parserVersion, dedupe string,
	now time.Time,
) (JobCreation, error) {
	version, err := catalog.Version(ctx, datID)
	if err != nil {
		return JobCreation{}, fmt.Errorf("read DAT version: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return JobCreation{}, fmt.Errorf("create DAT job ID: %w", err)
	}
	executionID, err := uuid.NewV7()
	if err != nil {
		return JobCreation{}, fmt.Errorf("create DAT execution ID: %w", err)
	}
	input, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "kind": "DAT_PARSE", "scope": map[string]any{"type": "DAT_VERSION", "id": datID},
		"executionId": executionID.String(), "inputs": map[string]any{
			"datVersion":    version,
			"datSha256":     datSHA,
			"parserVersion": parserVersion,
		},
	})
	if err != nil {
		return JobCreation{}, fmt.Errorf("encode DAT job input: %w", err)
	}
	digest := sha256.Sum256(input)
	return JobCreation{
		ID: id.String(), DATID: datID, DedupeKey: dedupe, Input: input, InputDigest: hex.EncodeToString(digest[:]),
		Payload: []byte(`{"schemaVersion":1,"inputExecutionNo":1}`), AtMS: now.UnixMilli(),
		Event: []byte(`{"schemaVersion":1,"executionNo":1,"attempt":0}`),
	}, nil
}

func claimBuiltInDATJob(ctx context.Context, repository Repository, datID, jobID string, now time.Time) error {
	err := repository.WithWrite(ctx, func(scope WriteScope) error {
		if err := scope.Jobs.Claim(ctx, JobClaim{
			JobID: jobID, DATID: datID, AtMS: now.UnixMilli(),
			DeadlineMS: now.Add(30 * time.Minute).UnixMilli(), LeaseUntilMS: now.Add(time.Minute).UnixMilli(),
			Event: []byte(`{"schemaVersion":1,"executionNo":1,"attempt":1}`),
		}); err != nil {
			return fmt.Errorf("claim DAT job: %w", err)
		}
		if err := scope.Catalog.MarkParsing(ctx, datID, now.UnixMilli()); err != nil {
			return fmt.Errorf("mark DAT parsing: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("claim built-in DAT: %w", err)
	}
	return nil
}

func failBuiltInDAT(ctx context.Context, repository Repository, datID, jobID, code string, now time.Time) error {
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
	err = repository.WithWrite(ctx, func(scope WriteScope) error {
		if err := scope.Catalog.MarkFailed(ctx, datID, now.UnixMilli()); err != nil {
			return fmt.Errorf("mark DAT failure: %w", err)
		}
		if err := scope.Jobs.Finish(
			ctx,
			JobFinish{
				JobID: jobID,
				DATID: datID,
				State: "FAILED",
				Code:  code,
				AtMS:  now.UnixMilli(),
				Event: event,
			},
		); err != nil {
			return fmt.Errorf("finish failed DAT job: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record built-in DAT failure: %w", err)
	}
	return nil
}

func recoverDATJob(ctx context.Context, records JobRecords, job Job, now int64) error {
	if job.State == "FAILED" || job.State == "CANCELLED" {
		return ErrDATParseFailed
	}
	if job.State == "RUNNING" {
		if err := records.Requeue(ctx, job.ID, now); err != nil {
			return fmt.Errorf("recover DAT job: %w", err)
		}
	}
	return nil
}
