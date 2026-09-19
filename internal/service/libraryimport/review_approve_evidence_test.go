package libraryimport

import (
	"encoding/json"
	"reflect"
	"testing"

	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
)

func TestReviewApprovalAuditRetainsV2ActorTagsAndBulkDecision(t *testing.T) {
	reason, cover, dos, candidate, dat := "确认发布", "cover", "GAME.EXE", "candidate", "dat"
	tags := []taggingmodel.Reference{{TagID: "tag", Name: "Selected"}}
	run := reviewApprovalRun{
		ctx:           authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"}),
		request:       model.ReviewApprovalRequest{ItemID: "item", Decision: model.ReviewApprovalDecision{Reason: &reason, DuplicatePolicy: "ALLOW_NEW"}, Bulk: &model.BulkPublicationIntent{BulkID: "bulk"}},
		head:          model.ReviewApprovalHead{MetadataJSON: `{"title":"Preserved"}`, UploadedCoverID: &cover, DraftDOS: &dos, CandidateID: &candidate, DATID: &dat},
		screenshotIDs: []string{"first", "second"}, screenshotOverride: true, publishedTags: tags,
		duplicateGames: []model.DuplicateGame{{GameID: "z"}, {GameID: "a"}}, gameID: "game", variantID: "variant", eventID: "event", now: 123,
	}
	event, err := run.evidence()
	if err != nil {
		t.Fatal(err)
	}
	assertApprovalActorEvidence(t, event, &reason)
	for _, raw := range []string{event.BeforeJSON, event.AfterJSON, event.DiffJSON, event.ConfigJSON, event.DATJSON, event.ProviderJSON} {
		var envelope struct{ SchemaVersion int }
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.SchemaVersion != 2 {
			t.Fatalf("audit=%s err=%v", raw, err)
		}
	}
	assertApprovalBeforeEvidence(t, event, tags)
	assertApprovalDiffEvidence(t, event, tags)
	assertApprovalAfterEvidence(t, event)
}

func assertApprovalAfterEvidence(t *testing.T, event model.ApprovalEvent) {
	t.Helper()
	var after struct {
		GameID, GameVariantID string
		Tags                  []taggingmodel.Reference
	}
	if err := json.Unmarshal([]byte(event.AfterJSON), &after); err != nil {
		t.Fatal(err)
	}
	if after.GameID != "game" || after.GameVariantID != "variant" || len(after.Tags) != 1 {
		t.Fatalf("after=%+v", after)
	}
	var config struct {
		Validation                string
		RuntimeScreenshotOverride bool
	}
	if err := json.Unmarshal([]byte(event.ConfigJSON), &config); err != nil {
		t.Fatal(err)
	}
	if config.Validation != "READY" || !config.RuntimeScreenshotOverride {
		t.Fatalf("config=%+v", config)
	}
	var provider struct {
		SelectedCandidateID *string
		CandidateSelected   bool
	}
	if err := json.Unmarshal([]byte(event.ProviderJSON), &provider); err != nil {
		t.Fatal(err)
	}
	if provider.SelectedCandidateID == nil || *provider.SelectedCandidateID != "candidate" || !provider.CandidateSelected {
		t.Fatalf("provider=%+v", provider)
	}
}

func TestReviewApprovalAuditSupportsSystemAndRejectsMalformedEvidence(t *testing.T) {
	run := reviewApprovalRun{ctx: t.Context(), head: model.ReviewApprovalHead{MetadataJSON: `{"title":"System"}`}, publishedTags: []taggingmodel.Reference{}}
	event, err := run.evidence()
	if err != nil || event.ActorKind != "SYSTEM" || event.ActorUserID != nil || event.ActorLabel == nil || *event.ActorLabel != "release-setup" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	run.head.MetadataJSON = "{"
	event, err = run.evidence()
	if err == nil || !reflect.DeepEqual(event, model.ApprovalEvent{}) {
		t.Fatalf("malformed event=%+v err=%v", event, err)
	}
}

func assertApprovalActorEvidence(t *testing.T, event model.ApprovalEvent, reason *string) {
	t.Helper()
	if event.ID != "event" || event.ItemID != "item" || event.ActorKind != "USER" || event.ActorUserID == nil || *event.ActorUserID != "actor" || event.ActorLabel != nil || event.Reason != reason || event.NowMS != 123 {
		t.Fatalf("event=%+v", event)
	}
}

func assertApprovalBeforeEvidence(t *testing.T, event model.ApprovalEvent, tags []taggingmodel.Reference) {
	t.Helper()
	var before approvalBeforeEvidence
	if err := json.Unmarshal([]byte(event.BeforeJSON), &before); err != nil {
		t.Fatal(err)
	}
	if string(before.Metadata) != `{"title":"Preserved"}` || !before.MediaSelection.Cover || before.MediaSelection.ScreenshotCount != 2 || before.DefaultDOS == nil || *before.DefaultDOS != "GAME.EXE" || !reflect.DeepEqual(before.Tags, tags) {
		t.Fatalf("before=%+v", before)
	}
}

func assertApprovalDiffEvidence(t *testing.T, event model.ApprovalEvent, tags []taggingmodel.Reference) {
	t.Helper()
	var diff approvalDiffEvidence
	if err := json.Unmarshal([]byte(event.DiffJSON), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.Decision != "APPROVED" || diff.ApprovalMode != "QUICK_STRICT_READY" || diff.BulkApprovalID != "bulk" || !diff.RuntimeScreenshotOverride || diff.DuplicatePolicy != "ALLOW_NEW" || !reflect.DeepEqual(diff.AcknowledgedGameIDs, []string{"a", "z"}) || !reflect.DeepEqual(diff.Tags, tags) {
		t.Fatalf("diff=%+v", diff)
	}
}
