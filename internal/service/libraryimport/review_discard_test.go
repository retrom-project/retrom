package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"
	"retrom/internal/model/importprogress"
	"retrom/internal/model/tagging"
)

type discardFixture struct {
	snapshot                                         model.ReviewDiscardSnapshot
	tags                                             []tagging.Reference
	snapshotError, tagError, commitError, writeError error
	failAt                                           string
	steps                                            []string
	event                                            model.ReviewDiscardEvent
	change                                           model.ReviewDiscardChange
	transactions                                     int
}

func (fixture *discardFixture) WithDiscard(_ context.Context, work func(model.ReviewDiscardScope) error) error {
	fixture.transactions++
	if err := work(model.ReviewDiscardScope{Reader: fixture, Tags: fixture, Writer: fixture, Payload: fixture.releaseScope()}); err != nil {
		return err
	}
	return fixture.commitError
}

func (fixture *discardFixture) CommitDiscard(_ context.Context, cmd model.DiscardCommand) (model.ReviewDecisionResult, error) {
	fixture.transactions++
	snapshot, found, snapshotErr := fixture.snapshot, fixture.snapshot.DraftID != "", fixture.snapshotError
	if snapshotErr != nil {
		return model.ReviewDecisionResult{}, snapshotErr
	}
	if !found || snapshot.Version != cmd.Request.ExpectedVersion ||
		snapshot.Version == math.MaxInt64 || snapshot.State != "REVIEW_PENDING" {
		return model.ReviewDecisionResult{}, model.ErrInvalid
	}
	if cmd.Request.Mode == model.ReviewDiscardSingle && (snapshot.SourceBusy || (snapshot.HandoffKind != "DIRECT" && !snapshot.EmulationStationReady)) {
		return model.ReviewDecisionResult{}, model.ErrInvalid
	}
	if fixture.tagError != nil {
		return model.ReviewDecisionResult{}, fixture.tagError
	}
	event := model.ReviewDiscardEvent{
		ID: cmd.EventID, ItemID: cmd.Request.ItemID, Reason: cmd.Request.Reason,
		NowMS: cmd.NowMS, ActorKind: cmd.Actor.Kind, ActorUserID: cmd.Actor.UserID, ActorLabel: cmd.Actor.Label,
	}
	beforeEvidence, _ := reviewDiscardEvidence(context.Background(), snapshot, fixture.tags, cmd.Request.Reason)
	event.BeforeJSON = beforeEvidence.BeforeJSON
	event.ConfigJSON = beforeEvidence.ConfigJSON
	event.DatJSON = beforeEvidence.DatJSON
	event.ProviderJSON = beforeEvidence.ProviderJSON
	fixture.event = event

	aggregate, err := projectReviewDiscardAggregate(snapshot.Aggregate, cmd.NowMS)
	if err != nil {
		return model.ReviewDecisionResult{}, err
	}
	change := model.ReviewDiscardChange{
		ItemID: cmd.Request.ItemID, ImportID: snapshot.ImportID,
		ExpectedVersion: cmd.Request.ExpectedVersion, NowMS: cmd.NowMS,
		Aggregate: aggregate,
	}
	for _, step := range []string{"attachments", "item", "event", "owner", "payload"} {
		fixture.steps = append(fixture.steps, step)
		if step == fixture.failAt {
			return model.ReviewDecisionResult{}, fixture.writeError
		}
	}
	fixture.change = change
	if fixture.commitError != nil {
		return model.ReviewDecisionResult{}, fixture.commitError
	}
	return model.ReviewDecisionResult{
		ItemID: cmd.Request.ItemID, EventID: cmd.EventID, Status: "DISCARDED",
		Version: snapshot.Version + 1, UpdatedAtMS: cmd.NowMS,
	}, nil
}

func (fixture *discardFixture) Snapshot(context.Context, string) (model.ReviewDiscardSnapshot, bool, error) {
	return fixture.snapshot, fixture.snapshot.DraftID != "", fixture.snapshotError
}

func (fixture *discardFixture) References(context.Context, tagging.Owner) ([]tagging.Reference, error) {
	return fixture.tags, fixture.tagError
}

func (fixture *discardFixture) step(step string) error {
	fixture.steps = append(fixture.steps, step)
	if step == fixture.failAt {
		return fixture.writeError
	}
	return nil
}

func (fixture *discardFixture) CancelAttachments(context.Context, string, int64) error {
	return fixture.step("attachments")
}

func (fixture *discardFixture) DiscardItem(_ context.Context, change model.ReviewDiscardChange) error {
	fixture.change = change
	return fixture.step("item")
}

func (fixture *discardFixture) RecordEvent(_ context.Context, event model.ReviewDiscardEvent) error {
	fixture.event = event
	return fixture.step("event")
}

func (fixture *discardFixture) TransitionOwner(context.Context, model.ReviewOwnerTransition) error {
	return fixture.step("owner")
}

func newDiscardFixture() *discardFixture {
	return &discardFixture{snapshot: model.ReviewDiscardSnapshot{
		DraftID: "draft", ImportID: "import", MetadataJSON: `{"title":"Retrom 自有"}`, Version: 1, State: "REVIEW_PENDING", HandoffKind: "DIRECT",
		Aggregate: model.ReviewDiscardAggregate{Version: 1, Progress: importprogress.Snapshot{
			State: "REVIEW_PENDING", Counts: importprogress.Counts{ReviewPending: 1},
		}},
	}, tags: []tagging.Reference{{TagID: "tag", Name: "Owned tag"}}}
}

func discardRequest() model.ReviewDiscardRequest {
	return model.ReviewDiscardRequest{ItemID: "item", ExpectedVersion: 1, Reason: " \n test reason \t "}
}

func discardService(fixture *discardFixture) *ReviewDiscards {
	service := NewReviewDiscards(fixture, func() time.Time { return time.UnixMilli(88) })
	service.newID = func() (string, error) { return "event", nil }
	return service
}

func TestReviewDiscardsPreservesV2EvidenceAndActor(t *testing.T) {
	t.Parallel()
	fixture := newDiscardFixture()
	validation, candidate, dat := "validation", "candidate", "dat"
	fixture.snapshot.ValidationID = &validation
	fixture.snapshot.CandidateID = &candidate
	fixture.snapshot.DatID = &dat
	fixture.snapshot.HasCover = true
	fixture.snapshot.HasBackground = true
	result, err := discardService(fixture).Discard(authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"}), discardRequest())
	if err != nil || result != (model.ReviewDecisionResult{ItemID: "item", EventID: "event", Status: "DISCARDED", Version: 2, UpdatedAtMS: 88}) {
		t.Fatalf("discard result=%+v err=%v", result, err)
	}
	event := fixture.event
	if event.ActorKind != "USER" || event.ActorUserID == nil || *event.ActorUserID != "actor" || event.ActorLabel != nil || event.Reason != "test reason" {
		t.Fatalf("discard actor/reason=%+v", event)
	}
	assertDiscardV2Event(t, event, fixture.snapshot, fixture.tags)
	if !reflect.DeepEqual(fixture.steps, []string{"attachments", "item", "event", "owner", "payload"}) {
		t.Fatalf("discard ordering=%v", fixture.steps)
	}
}

func TestReviewDiscardsRejectsInvalidInputsBeforeTransaction(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*model.ReviewDiscardRequest)
	}{
		{"missing item", func(r *model.ReviewDiscardRequest) { r.ItemID = "" }},
		{"zero version", func(r *model.ReviewDiscardRequest) { r.ExpectedVersion = 0 }},
		{"unknown mode", func(r *model.ReviewDiscardRequest) { r.Mode = "BYPASS" }},
		{"too many runes", func(r *model.ReviewDiscardRequest) { r.Reason = strings.Repeat("中", 501) }},
		{"invalid UTF8", func(r *model.ReviewDiscardRequest) { r.Reason = string([]byte{255}) }},
		{"control character", func(r *model.ReviewDiscardRequest) { r.Reason = "bad\x00reason" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDiscardFixture()
			request := discardRequest()
			test.change(&request)
			result, err := discardService(fixture).Discard(t.Context(), request)
			if !errors.Is(err, model.ErrInvalid) || result != (model.ReviewDecisionResult{}) || fixture.transactions != 0 {
				t.Fatalf("invalid request reached transaction: %+v err=%v tx=%d", result, err, fixture.transactions)
			}
		})
	}
}

func TestReviewDiscardsRechecksModeAndAuthority(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		change  func(*model.ReviewDiscardSnapshot)
		mode    model.ReviewDiscardMode
		allowed bool
	}{
		{"stale version", func(s *model.ReviewDiscardSnapshot) { s.Version = 2 }, model.ReviewDiscardSingle, false},
		{"version overflow", func(s *model.ReviewDiscardSnapshot) { s.Version = math.MaxInt64 }, model.ReviewDiscardSingle, false},
		{"already published", func(s *model.ReviewDiscardSnapshot) { s.State = "PUBLISHED" }, model.ReviewDiscardBatch, false},
		{"missing draft", func(s *model.ReviewDiscardSnapshot) { s.DraftID = "" }, model.ReviewDiscardSingle, false},
		{"single reserved", func(s *model.ReviewDiscardSnapshot) { s.HandoffKind = "EMULATIONSTATION" }, model.ReviewDiscardSingle, false},
		{"single busy owner", func(s *model.ReviewDiscardSnapshot) { s.SourceBusy = true }, model.ReviewDiscardSingle, false},
		{"single ready reservation", func(s *model.ReviewDiscardSnapshot) {
			s.HandoffKind = "EMULATIONSTATION"
			s.EmulationStationReady = true
		}, model.ReviewDiscardSingle, true},
		{"batch reserved", func(s *model.ReviewDiscardSnapshot) { s.HandoffKind = "EMULATIONSTATION"; s.SourceBusy = true }, model.ReviewDiscardBatch, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDiscardFixture()
			test.change(&fixture.snapshot)
			request := discardRequest()
			request.Mode = test.mode
			if test.name == "version overflow" {
				request.ExpectedVersion = math.MaxInt64
			}
			result, err := discardService(fixture).Discard(t.Context(), request)
			if test.allowed {
				if err != nil || result.EventID == "" {
					t.Fatalf("authorized discard rejected: %+v err=%v", result, err)
				}
				return
			}
			if !errors.Is(err, model.ErrInvalid) || result != (model.ReviewDecisionResult{}) || len(fixture.steps) != 0 {
				t.Fatalf("unauthorized discard mutated: %+v err=%v steps=%v", result, err, fixture.steps)
			}
		})
	}
}

func TestReviewDiscardsFailurePreservesCauseAndClearsResult(t *testing.T) {
	t.Parallel()
	cause := errors.New("discard boundary failed")
	for _, stage := range []string{"snapshot", "tags", "id", "attachments", "item", "event", "owner", "payload", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			fixture := newDiscardFixture()
			service := discardService(fixture)
			switch stage {
			case "snapshot":
				fixture.snapshotError = cause
			case "tags":
				fixture.tagError = cause
			case "id":
				service.newID = func() (string, error) { return "", cause }
			case "commit":
				fixture.commitError = cause
			default:
				fixture.failAt = stage
				fixture.writeError = cause
			}
			result, err := service.Discard(t.Context(), discardRequest())
			if !errors.Is(err, cause) || errors.Is(err, model.ErrInvalid) || result != (model.ReviewDecisionResult{}) {
				t.Fatalf("failure lost cause/leaked success: %+v err=%v", result, err)
			}
			if (stage == "snapshot" || stage == "tags" || stage == "id") && len(fixture.steps) != 0 {
				t.Fatalf("preparation failure wrote: %v", fixture.steps)
			}
		})
	}
}

func TestReviewDiscardsReasonBoundaryAndSystemActor(t *testing.T) {
	t.Parallel()
	fixture := newDiscardFixture()
	request := discardRequest()
	request.Reason = strings.Repeat("中", 497) + "\n\tX"
	result, err := discardService(fixture).Discard(t.Context(), request)
	if err != nil || result.EventID == "" || fixture.event.ActorKind != "SYSTEM" || fixture.event.ActorUserID != nil || fixture.event.ActorLabel == nil || *fixture.event.ActorLabel != "release-setup" {
		t.Fatalf("valid multiline reason/system actor rejected: %+v event=%+v err=%v", result, fixture.event, err)
	}
}

func assertDiscardV2Event(t *testing.T, event model.ReviewDiscardEvent, snapshot model.ReviewDiscardSnapshot, tags []tagging.Reference) {
	t.Helper()
	var before discardedReviewEvidence
	if err := json.Unmarshal([]byte(event.BeforeJSON), &before); err != nil {
		t.Fatal(err)
	}
	if before.SchemaVersion != 2 || string(before.Metadata) != snapshot.MetadataJSON || !reflect.DeepEqual(before.Tags, tags) || !before.MediaSelection.Cover || !before.MediaSelection.Background {
		t.Fatalf("discard evidence changed: %+v", before)
	}
	if event.ConfigJSON != `{"schemaVersion":2,"validationAvailable":true}` || event.DatJSON != `{"schemaVersion":2,"datMatched":true}` || event.ProviderJSON != `{"schemaVersion":2,"selectedCandidateId":"candidate","candidateSelected":true}` {
		t.Fatalf("auxiliary v2 evidence changed: %+v", event)
	}
}
