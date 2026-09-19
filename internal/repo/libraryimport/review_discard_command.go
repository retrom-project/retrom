package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"retrom/internal/model/importprogress"
	application "retrom/internal/model/libraryimport"
	payloadmodel "retrom/internal/model/payloadrelease"
	taggingmodel "retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
	payloadrepo "retrom/internal/repo/payloadrelease"
	tagpersistence "retrom/internal/repo/tagging"
)

func (repository *ReviewDiscards) CommitDiscard(
	ctx context.Context, cmd application.DiscardCommand,
) (application.ReviewDecisionResult, error) {
	var result application.ReviewDecisionResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		var err error
		result, err = applyDiscard(ctx, executor, cmd)
		return err
	})
	if err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("commit review discard: %w", err)
	}
	return result, nil
}

func applyDiscard(
	ctx context.Context, executor dbexec.Executor, cmd application.DiscardCommand,
) (application.ReviewDecisionResult, error) {
	records := reviewDiscardRecords{executor: executor}
	request := cmd.Request

	snapshot, found, err := records.Snapshot(ctx, request.ItemID)
	if err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("read discard evidence: %w", err)
	}
	if !found || !canDiscardSnapshot(snapshot, request) {
		return application.ReviewDecisionResult{}, application.ErrInvalid
	}

	tags, err := tagpersistence.BindReferenceReader(executor).References(
		ctx, taggingmodel.Owner{Kind: taggingmodel.OwnerReviewDraft, ID: snapshot.DraftID},
	)
	if err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("read discard tags: %w", err)
	}

	event, err := buildDiscardEvent(snapshot, tags, request.Reason)
	if err != nil {
		return application.ReviewDecisionResult{}, err
	}
	event.ID = cmd.EventID
	event.ItemID = request.ItemID
	event.NowMS = cmd.NowMS
	event.ActorKind = cmd.Actor.Kind
	event.ActorUserID = cmd.Actor.UserID
	event.ActorLabel = cmd.Actor.Label

	aggregate, err := projectDiscardAggregate(snapshot.Aggregate, cmd.NowMS)
	if err != nil {
		return application.ReviewDecisionResult{}, err
	}

	change := application.ReviewDiscardChange{
		ItemID: request.ItemID, ImportID: snapshot.ImportID,
		ExpectedVersion: request.ExpectedVersion, NowMS: cmd.NowMS,
		Aggregate: aggregate,
	}

	if err := records.CancelAttachments(ctx, request.ItemID, cmd.NowMS); err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("cancel discarded attachments: %w", err)
	}
	if err := records.DiscardItem(ctx, change); err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("discard review and aggregate: %w", err)
	}
	if err := records.RecordEvent(ctx, event); err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("record discarded review: %w", err)
	}
	if err := TransitionReviewOwners(ctx, executor, application.ReviewOwnerTransition{
		ItemID: request.ItemID, State: application.ReviewOwnerDiscarded,
		Mode: request.Mode, NowMS: cmd.NowMS,
	}); err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("transition discarded review owner: %w", err)
	}

	scope := payloadrepo.BindReleases(executor)
	if err := payloadrepo.NewScheduler(nil).Review(ctx, scope, payloadmodel.ReviewRelease{
		ItemID: request.ItemID, ImportID: snapshot.ImportID,
		Reason: payloadmodel.ReasonImportDiscarded, NowMS: cmd.NowMS,
	}); err != nil {
		return application.ReviewDecisionResult{}, fmt.Errorf("schedule discarded review payload: %w", err)
	}

	return application.ReviewDecisionResult{
		ItemID: request.ItemID, EventID: cmd.EventID, Status: "DISCARDED",
		Version: snapshot.Version + 1, UpdatedAtMS: cmd.NowMS,
	}, nil
}

func canDiscardSnapshot(snapshot application.ReviewDiscardSnapshot, request application.ReviewDiscardRequest) bool {
	if snapshot.Version != request.ExpectedVersion || snapshot.Version == math.MaxInt64 ||
		snapshot.State != "REVIEW_PENDING" {
		return false
	}
	if request.Mode == application.ReviewDiscardBatch {
		return true
	}
	return !snapshot.SourceBusy && (snapshot.HandoffKind == "DIRECT" || snapshot.EmulationStationReady)
}

func projectDiscardAggregate(
	before application.ReviewDiscardAggregate, now int64,
) (application.ReviewDiscardAggregateChange, error) {
	if before.Version < 1 || before.Version == math.MaxInt64 || before.Progress.Counts.ReviewPending < 1 {
		return application.ReviewDiscardAggregateChange{}, application.ErrInvalid
	}
	progress := before.Progress
	progress.Started = true
	progress.Counts.ReviewPending--
	projection, err := importprogress.Project(progress, now)
	if err != nil {
		return application.ReviewDiscardAggregateChange{}, fmt.Errorf("project discarded import progress: %w", err)
	}
	return application.ReviewDiscardAggregateChange{
		ExpectedVersion: before.Version, ExpectedPending: before.Progress.Counts.ReviewPending,
		Projection: projection,
	}, nil
}

type discardedReviewEvidence struct {
	SchemaVersion  int                      `json:"schemaVersion"`
	Metadata       json.RawMessage          `json:"metadata"`
	Tags           []taggingmodel.Reference `json:"tags"`
	MediaSelection struct {
		Cover      bool `json:"cover"`
		Background bool `json:"background"`
	} `json:"mediaSelection"`
}

func buildDiscardEvent(
	snapshot application.ReviewDiscardSnapshot,
	tags []taggingmodel.Reference,
	reason string,
) (application.ReviewDiscardEvent, error) {
	before := discardedReviewEvidence{SchemaVersion: 2, Metadata: json.RawMessage(snapshot.MetadataJSON), Tags: tags}
	before.MediaSelection.Cover = snapshot.HasCover
	before.MediaSelection.Background = snapshot.HasBackground
	encoded, err := json.Marshal(before)
	if err != nil {
		return application.ReviewDiscardEvent{}, fmt.Errorf("encode discarded review evidence: %w", err)
	}
	event := application.ReviewDiscardEvent{Reason: reason, BeforeJSON: string(encoded)}
	encoded, err = json.Marshal(struct {
		SchemaVersion       int  `json:"schemaVersion"`
		ValidationAvailable bool `json:"validationAvailable"`
	}{2, snapshot.ValidationID != nil})
	if err != nil {
		return application.ReviewDiscardEvent{}, fmt.Errorf("encode discarded validation evidence: %w", err)
	}
	event.ConfigJSON = string(encoded)
	encoded, err = json.Marshal(struct {
		SchemaVersion int  `json:"schemaVersion"`
		DatMatched    bool `json:"datMatched"`
	}{2, snapshot.DatID != nil})
	if err != nil {
		return application.ReviewDiscardEvent{}, fmt.Errorf("encode discarded DAT evidence: %w", err)
	}
	event.DatJSON = string(encoded)
	encoded, err = json.Marshal(struct {
		SchemaVersion       int     `json:"schemaVersion"`
		SelectedCandidateID *string `json:"selectedCandidateId"`
		CandidateSelected   bool    `json:"candidateSelected"`
	}{2, snapshot.CandidateID, snapshot.CandidateID != nil})
	if err != nil {
		return application.ReviewDiscardEvent{}, fmt.Errorf("encode discarded provider evidence: %w", err)
	}
	event.ProviderJSON = string(encoded)
	return event, nil
}
