package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/uploads"

	"retrom/internal/foundation/cleanup"

	"github.com/google/uuid"
)

func (service *Service) Complete(ctx context.Context, id string, version int64) (string, int64, error) {
	var run model.Run
	err := service.repository.CommitWrite(ctx, func(scope model.WriteScope) error {
		current, err := scope.Sessions.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read upload completion state: %w", err)
		}
		if current.Version != version || current.Consumed ||
			current.State != "UPLOADING" && current.State != "CREATED" && current.State != "FAILED" {
			return model.ErrInvalid
		}
		files, err := scope.Finalize.Manifest(ctx, id)
		if err != nil {
			return fmt.Errorf("freeze upload parts: %w", err)
		}
		job, err := prepareFinalization(id, current.FinalizationNo+1, service.now().UnixMilli(), files)
		if err != nil {
			return err
		}
		if err := scope.Jobs.Create(ctx, job); err != nil {
			return fmt.Errorf("create finalize job: %w", err)
		}
		if err := scope.Sessions.BeginFinalization(
			ctx,
			model.Finalization{
				Run:             job.Run,
				ExpectedVersion: version,
				AtMS:            job.AtMS,
			},
		); err != nil {
			return fmt.Errorf("begin upload finalization: %w", err)
		}
		if err := scope.Files.MarkFinalizing(ctx, id, job.AtMS); err != nil {
			return fmt.Errorf("mark upload files finalizing: %w", err)
		}
		run = job.Run
		return nil
	})
	if err != nil {
		return "", 0, fmt.Errorf("complete upload: %w", err)
	}
	service.Resume(ctx, run.JobID)
	return run.JobID, run.FinalizationNo, nil
}

func prepareFinalization(uploadID string, number, now int64, files []model.FrozenFile) (model.JobCreation, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.JobCreation{}, fmt.Errorf("generate finalization job ID: %w", err)
	}
	execution, err := uuid.NewV7()
	if err != nil {
		return model.JobCreation{}, fmt.Errorf("generate finalization execution ID: %w", err)
	}
	run := model.Run{UploadID: uploadID, JobID: id.String(), FinalizationNo: number, ExecutionNo: 1}
	envelope := model.FinalizationInput{SchemaVersion: 1, Kind: "UPLOAD_FINALIZE", ExecutionID: execution.String()}
	envelope.Scope.Type = "UPLOAD_SESSION"
	envelope.Scope.ID = uploadID
	envelope.Inputs.FinalizationNo = number
	envelope.Inputs.Files = files
	input, err := json.Marshal(envelope)
	if err != nil {
		return model.JobCreation{}, fmt.Errorf("encode finalization input: %w", err)
	}
	inputDigest := sha256.Sum256(input)
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", uploadID, number)))
	return model.JobCreation{
		Run: run, DedupeKey: hex.EncodeToString(dedupe[:]), InputDigest: hex.EncodeToString(inputDigest[:]),
		InputJSON: input, PayloadJSON: []byte(fmt.Sprintf(`{"uploadId":%q,"finalizationNo":%d}`, uploadID, number)),
		EventJSON: jobEvent(run, 0, ""), AtMS: now,
	}, nil
}

func jobEvent(run model.Run, attempt int, code string) []byte {
	data, _ := json.Marshal(
		map[string]any{
			"schemaVersion": 1,
			"executionNo":   run.ExecutionNo,
			"attempt":       attempt,
			"errorCode":     code,
		},
	)
	return data
}

func (service *Service) Cancel(ctx context.Context, id string, version int64) (model.Canceled, bool, error) {
	var result model.Canceled
	var pending bool
	err := service.repository.CommitWrite(ctx, func(scope model.WriteScope) error {
		current, err := scope.Sessions.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read upload cancellation state: %w", err)
		}
		if current.Version != version || current.Consumed || current.State == "COMPLETE" ||
			current.State == "CANCELLED" || current.State == "EXPIRED" {
			return model.ErrInvalid
		}
		now := service.now().UnixMilli()
		if current.State == "FINALIZING" {
			pending, err = requestFinalizeCancellation(ctx, scope.Jobs, current, now)
			if err != nil {
				return err
			}
		}
		if pending {
			if err := scope.Sessions.Advance(
				ctx,
				model.SessionProgress{
					ID:              id,
					State:           current.State,
					ExpectedVersion: version,
					AtMS:            now,
				},
			); err != nil {
				return fmt.Errorf("persist upload transition: %w", err)
			}
		} else {
			if err := finishUploadCancellation(ctx, scope, model.Run{UploadID: id}, version, now); err != nil {
				return err
			}
		}
		result = model.Canceled{UploadID: id, State: "CANCELLED", Version: version + 1}
		if pending {
			result.State = "CANCEL_REQUESTED"
		}
		return nil
	})
	if err != nil {
		return model.Canceled{}, false, fmt.Errorf("cancel upload: %w", err)
	}
	if !pending {
		cleanup.Error("remove cancelled upload parts", service.cleanupUpload(ctx, id, ""))
	}
	return result, pending, nil
}

func requestFinalizeCancellation(
	ctx context.Context,
	records model.JobRecords,
	current model.SessionState,
	now int64,
) (bool, error) {
	if current.FinalizeJobID == nil {
		return false, model.ErrInvalid
	}
	job, err := records.Get(ctx, *current.FinalizeJobID)
	if err != nil {
		return false, fmt.Errorf("read finalize cancellation target: %w", err)
	}
	if job.State == "CANCEL_REQUESTED" {
		return true, nil
	}
	if job.State == "CANCELLED" {
		return false, nil
	}
	if job.State != "QUEUED" && job.State != "RUNNING" {
		return false, model.ErrInvalid
	}
	input := model.JobCancellation{
		ID:            job.ID,
		UploadID:      current.ID,
		ExpectedState: job.State,
		State:         "CANCEL_REQUESTED",
		AtMS:          now,
		ExecutionNo:   job.ExecutionNo,
	}
	if job.State == "QUEUED" {
		input.State = "CANCELLED"
		input.FinishedAtMS = &now
	}
	input.EventJSON = jobEvent(model.Run{ExecutionNo: job.ExecutionNo}, 0, "")
	if err := records.RequestCancel(ctx, input); err != nil {
		return false, fmt.Errorf("request upload cancellation: %w", err)
	}
	return job.State == "RUNNING", nil
}

func finishUploadCancellation(ctx context.Context, scope model.WriteScope, run model.Run, version, now int64) error {
	code := "UPLOAD_CANCELLED"
	if err := scope.Sessions.Finish(
		ctx,
		model.SessionFinish{
			Run:             run,
			State:           "CANCELLED",
			ExpectedVersion: version,
			AtMS:            now,
			ErrorCode:       &code,
		},
	); err != nil {
		return fmt.Errorf("persist upload transition: %w", err)
	}
	failure := model.PendingFailure{UploadID: run.UploadID, Code: code, AtMS: now}
	if err := scope.Files.FailPending(ctx, failure); err != nil {
		return fmt.Errorf("cancel unfinished upload files: %w", err)
	}
	return nil
}
