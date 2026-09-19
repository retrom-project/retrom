package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	librarymodel "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	libraryrepo "retrom/internal/repo/libraryimport"
)

type ReviewHandoff struct{ database *sql.DB }

func NewReviewHandoff(database *sql.DB) *ReviewHandoff { return &ReviewHandoff{database: database} }

func (repository *ReviewHandoff) CommitReviewHandoff(
	ctx context.Context,
	request application.ReviewHandoffRequest,
	nowMS int64,
	auditID, actorKind string,
	actorUserID, actorLabel *string,
) error {
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := reviewHandoffRecords{executor: executor}
		before, found, err := records.Current(ctx, request.Execution.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation execution: %w", err)
		}
		if !found {
			return application.ErrVersionConflict
		}
		if err := application.ValidateImportExecution(before, request.Execution, nowMS); err != nil {
			return err
		}
		review, found, err := records.Review(ctx, request.Execution.ImportID, request.ItemID)
		if err != nil {
			return fmt.Errorf("read EmulationStation review reservation: %w", err)
		}
		if !found || !application.MatchesReviewHandoff(review, request) {
			return application.ErrVersionConflict
		}
		if review.State == "REVIEW_PENDING" {
			return nil
		}
		if review.State != "COPYING" && review.State != "VALIDATING" {
			return application.ErrVersionConflict
		}
		preparation, err := application.ReviewPreparation(review)
		if err != nil {
			return err
		}

		execRecords := executionRecords{executor: executor}
		if err := execRecords.Fence(ctx, before, nowMS); err != nil {
			return fmt.Errorf("fence EmulationStation metadata handoff: %w", err)
		}

		var metadata librarymodel.ServerMetadata
		if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
			return fmt.Errorf("decode frozen EmulationStation review metadata: %w", err)
		}
		input := librarymodel.MetadataSeedInput{
			ItemID: review.ReservedItemID, Metadata: metadata,
			MaximumYear: before.ReleaseYearMax, NowMS: nowMS,
			AuditID: auditID, ActorKind: actorKind,
			ActorUserID: actorUserID, ActorLabel: actorLabel,
		}
		_, additions, err := libraryrepo.SeedMetadata(ctx, executor, input)
		if err != nil {
			return fmt.Errorf("seed EmulationStation review metadata: %w", err)
		}
		warningsJSON, err := appendExecutionWarningsValue(review.WarningsJSON, additions)
		if err != nil {
			return err
		}
		if err := execRecords.CompleteReview(ctx, application.ExecutionReviewCompletion{
			Before: before, Review: review, WarningsJSON: warningsJSON,
			Preparation: preparation, NowMS: nowMS,
		}); err != nil {
			return fmt.Errorf("save EmulationStation review handoff: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete EmulationStation review handoff: %w", err)
	}
	return nil
}

type reviewHandoffRecords struct {
	executor dbexec.Executor
}

func (records reviewHandoffRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return (executionRecords{executor: records.executor}).Current(ctx, id)
}

func (records reviewHandoffRecords) Review(
	ctx context.Context,
	importID, id string,
) (application.ExecutionReview, bool, error) {
	review, err := scanExecutionReview(records.executor.QueryRowContext(ctx, executionReviewSQL+`
WHERE source.id=? AND source.import_id=?
AND item.state='REVIEW_PENDING' AND item.review_handoff_kind='EMULATIONSTATION'`, id, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return application.ExecutionReview{}, false, nil
	}
	if err != nil {
		return application.ExecutionReview{}, false, fmt.Errorf("read EmulationStation review handoff: %w", err)
	}
	return review, true, nil
}

func appendExecutionWarningsValue(encoded string, additions []librarymodel.ServerMetadataWarning) (string, error) {
	var warnings []map[string]any
	if err := json.Unmarshal([]byte(encoded), &warnings); err != nil {
		return "", fmt.Errorf("decode EmulationStation warnings: %w", err)
	}
	if warnings == nil {
		return "", application.ErrInvalid
	}
	for _, addition := range additions {
		found := false
		for _, existing := range warnings {
			if existing["code"] == addition.Code && existing["field"] == addition.Field {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, map[string]any{"code": addition.Code, "field": addition.Field})
		}
	}
	warnings = application.BoundedWarnings(warnings)
	value, err := json.Marshal(warnings)
	if err != nil {
		return "", fmt.Errorf("encode EmulationStation warnings: %w", err)
	}
	return string(value), nil
}
