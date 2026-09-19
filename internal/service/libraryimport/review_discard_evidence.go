package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
)

type discardedReviewEvidence struct {
	SchemaVersion  int                      `json:"schemaVersion"`
	Metadata       json.RawMessage          `json:"metadata"`
	Tags           []taggingmodel.Reference `json:"tags"`
	MediaSelection struct {
		Cover      bool `json:"cover"`
		Background bool `json:"background"`
	} `json:"mediaSelection"`
}

func reviewDiscardEvidence(
	ctx context.Context, snapshot model.ReviewDiscardSnapshot, tags []taggingmodel.Reference, reason string,
) (model.ReviewDiscardEvent, error) {
	before := discardedReviewEvidence{SchemaVersion: 2, Metadata: json.RawMessage(snapshot.MetadataJSON), Tags: tags}
	before.MediaSelection.Cover = snapshot.HasCover
	before.MediaSelection.Background = snapshot.HasBackground
	encoded, err := json.Marshal(before)
	if err != nil {
		return model.ReviewDiscardEvent{}, fmt.Errorf("encode discarded review evidence: %w", err)
	}
	event := model.ReviewDiscardEvent{Reason: reason, BeforeJSON: string(encoded), ActorKind: "SYSTEM"}
	encoded, err = json.Marshal(struct {
		SchemaVersion       int  `json:"schemaVersion"`
		ValidationAvailable bool `json:"validationAvailable"`
	}{2, snapshot.ValidationID != nil})
	if err != nil {
		return model.ReviewDiscardEvent{}, fmt.Errorf("encode discarded validation evidence: %w", err)
	}
	event.ConfigJSON = string(encoded)
	encoded, err = json.Marshal(struct {
		SchemaVersion int  `json:"schemaVersion"`
		DatMatched    bool `json:"datMatched"`
	}{2, snapshot.DatID != nil})
	if err != nil {
		return model.ReviewDiscardEvent{}, fmt.Errorf("encode discarded DAT evidence: %w", err)
	}
	event.DatJSON = string(encoded)
	encoded, err = json.Marshal(struct {
		SchemaVersion       int     `json:"schemaVersion"`
		SelectedCandidateID *string `json:"selectedCandidateId"`
		CandidateSelected   bool    `json:"candidateSelected"`
	}{2, snapshot.CandidateID, snapshot.CandidateID != nil})
	if err != nil {
		return model.ReviewDiscardEvent{}, fmt.Errorf("encode discarded provider evidence: %w", err)
	}
	event.ProviderJSON = string(encoded)
	label := "release-setup"
	event.ActorLabel = &label
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		event.ActorKind = "USER"
		event.ActorUserID = &principal.UserID
		event.ActorLabel = nil
	}
	return event, nil
}
