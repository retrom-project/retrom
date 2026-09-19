package libraryimport

import (
	"encoding/json"
	"fmt"

	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
)

type approvalBeforeEvidence struct {
	SchemaVersion  int                      `json:"schemaVersion"`
	Metadata       json.RawMessage          `json:"metadata"`
	MediaSelection approvalMediaEvidence    `json:"mediaSelection"`
	DefaultDOS     *string                  `json:"defaultDosEntry"`
	Tags           []taggingmodel.Reference `json:"tags"`
}
type approvalMediaEvidence struct {
	Cover           bool `json:"cover"`
	Background      bool `json:"background"`
	ScreenshotCount int  `json:"screenshotCount"`
}
type approvalDiffEvidence struct {
	SchemaVersion             int                      `json:"schemaVersion"`
	Decision                  string                   `json:"decision"`
	Tags                      []taggingmodel.Reference `json:"tags"`
	MediaChanged              bool                     `json:"mediaChanged"`
	ApprovalMode              string                   `json:"approvalMode,omitempty"`
	BulkApprovalID            string                   `json:"bulkApprovalId,omitempty"`
	RuntimeScreenshotOverride bool                     `json:"runtimeScreenshotOverride,omitempty"`
	DuplicatePolicy           string                   `json:"duplicatePolicy,omitempty"`
	AcknowledgedGameIDs       []string                 `json:"acknowledgedGameIds,omitempty"`
}

func (run *reviewApprovalRun) evidence() (model.ApprovalEvent, error) {
	event := model.ApprovalEvent{
		ID: run.eventID, ItemID: run.request.ItemID,
		Reason: run.request.Decision.Reason, NowMS: run.now,
	}
	media := approvalMediaEvidence{
		Cover:      hasApprovalText(run.head.CoverID) || run.head.UploadedCoverID != nil,
		Background: hasApprovalText(run.head.BackgroundID), ScreenshotCount: len(run.screenshotIDs),
	}
	before := approvalBeforeEvidence{
		SchemaVersion: 2, Metadata: json.RawMessage(run.head.MetadataJSON),
		MediaSelection: media, DefaultDOS: run.head.DraftDOS, Tags: run.publishedTags,
	}
	if err := encodeApprovalEvidence(before, &event.BeforeJSON); err != nil {
		return model.ApprovalEvent{}, err
	}
	after := struct {
		SchemaVersion int                      `json:"schemaVersion"`
		GameID        string                   `json:"gameId"`
		VariantID     string                   `json:"gameVariantId"`
		Tags          []taggingmodel.Reference `json:"tags"`
	}{2, run.gameID, run.variantID, run.publishedTags}
	if err := encodeApprovalEvidence(after, &event.AfterJSON); err != nil {
		return model.ApprovalEvent{}, err
	}
	if err := encodeApprovalEvidence(run.diffEvidence(media), &event.DiffJSON); err != nil {
		return model.ApprovalEvent{}, err
	}
	config := struct {
		SchemaVersion             int    `json:"schemaVersion"`
		Validation                string `json:"validation"`
		RuntimeScreenshotOverride bool   `json:"runtimeScreenshotOverride"`
	}{2, "READY", run.screenshotOverride}
	if err := encodeApprovalEvidence(config, &event.ConfigJSON); err != nil {
		return model.ApprovalEvent{}, err
	}
	dat := struct {
		SchemaVersion int  `json:"schemaVersion"`
		Matched       bool `json:"datMatched"`
	}{2, run.head.DATID != nil}
	if err := encodeApprovalEvidence(dat, &event.DATJSON); err != nil {
		return model.ApprovalEvent{}, err
	}
	provider := struct {
		SchemaVersion int     `json:"schemaVersion"`
		CandidateID   *string `json:"selectedCandidateId"`
		Selected      bool    `json:"candidateSelected"`
	}{2, run.head.CandidateID, run.head.CandidateID != nil}
	if err := encodeApprovalEvidence(provider, &event.ProviderJSON); err != nil {
		return model.ApprovalEvent{}, err
	}
	event.ActorKind = "SYSTEM"
	label := "release-setup"
	event.ActorLabel = &label
	if principal, ok := authn.PrincipalFromContext(run.ctx); ok && principal.UserID != "" {
		event.ActorKind, event.ActorUserID, event.ActorLabel = "USER", &principal.UserID, nil
	}
	return event, nil
}

func (run *reviewApprovalRun) diffEvidence(media approvalMediaEvidence) approvalDiffEvidence {
	diff := approvalDiffEvidence{
		SchemaVersion: 2, Decision: "APPROVED", Tags: run.publishedTags,
		MediaChanged:              media.Cover || media.Background || media.ScreenshotCount > 0,
		RuntimeScreenshotOverride: run.screenshotOverride,
	}
	if run.request.Bulk != nil {
		diff.ApprovalMode, diff.BulkApprovalID = "QUICK_STRICT_READY", run.request.Bulk.BulkID
	}
	if run.request.Decision.DuplicatePolicy == "ALLOW_NEW" {
		diff.DuplicatePolicy = "ALLOW_NEW"
		diff.AcknowledgedGameIDs = ApprovalDuplicateIDs(run.duplicateGames)
	}
	return diff
}

func encodeApprovalEvidence[T any](value T, destination *string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode approval evidence: %w", err)
	}
	*destination = string(encoded)
	return nil
}

func hasApprovalText(value *string) bool { return value != nil && *value != "" }
