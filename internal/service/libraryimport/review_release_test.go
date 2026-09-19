package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"testing"

	model "retrom/internal/model/libraryimport"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
)

type discardReleaseRepository struct {
	*discardFixture
	cause error
}

func (fixture discardReleaseRepository) WithDiscard(_ context.Context, work func(model.ReviewDiscardScope) error) error {
	return work(model.ReviewDiscardScope{
		Reader: fixture.discardFixture, Tags: fixture.discardFixture, Writer: fixture.discardFixture,
		Payload: payloadreleasemodel.ReleaseScope{Scheduling: failedReviewScheduling{cause: fixture.cause}},
	})
}

type failedReviewScheduling struct {
	payloadreleasemodel.SchedulingScope

	cause error
}

func (scope failedReviewScheduling) Owner(context.Context, payloadreleasemodel.Scope) (payloadreleasemodel.Owner, error) {
	return payloadreleasemodel.Owner{}, scope.cause
}

func TestReviewDiscardUsesTypedPayloadScopeAndPreservesCause(t *testing.T) {
	fixture := newDiscardFixture()
	cause := errors.New("release owner read failed")
	service := discardService(fixture)
	service.repository = discardReleaseRepository{discardFixture: fixture, cause: cause}
	result, err := service.Discard(t.Context(), discardRequest())
	if !errors.Is(err, cause) || result != (model.ReviewDecisionResult{}) {
		t.Fatalf("typed payload failure result=%+v error=%v", result, err)
	}
	if !reflect.DeepEqual(fixture.steps, []string{"attachments", "item", "event", "owner"}) {
		t.Fatalf("writes before payload read=%v", fixture.steps)
	}
}

func (fixture *discardFixture) releaseScope() payloadreleasemodel.ReleaseScope {
	scope := discardReleaseScope{fixture: fixture}
	return payloadreleasemodel.ReleaseScope{Scheduling: scope, Links: scope}
}

type discardReleaseScope struct{ fixture *discardFixture }

func (scope discardReleaseScope) Owner(_ context.Context, owner payloadreleasemodel.Scope) (payloadreleasemodel.Owner, error) {
	state := "COMPLETED"
	if owner.Type == payloadreleasemodel.ScopeImportItem {
		if err := scope.fixture.step("payload"); err != nil {
			return payloadreleasemodel.Owner{}, err
		}
		state = "DISCARDED"
	}
	return payloadreleasemodel.Owner{Scope: owner, State: state, PayloadState: "RELEASED", Version: 1, ReleaseJobID: "release"}, nil
}
func (discardReleaseScope) PendingChildren(context.Context, string) (int64, error) { return 0, nil }
func (discardReleaseScope) Consumption(context.Context, string) (payloadreleasemodel.Consumption, error) {
	return payloadreleasemodel.Consumption{}, errors.New("unexpected consumption read")
}

func (discardReleaseScope) CreateJob(context.Context, payloadreleasemodel.ScheduledJob) error {
	return errors.New("unexpected release job creation")
}

func (discardReleaseScope) BeginRelease(context.Context, payloadreleasemodel.OwnerRelease) error {
	return errors.New("unexpected release projection")
}

func (discardReleaseScope) RetainedSources(context.Context, payloadreleasemodel.SourceBatch, string, int) ([]string, error) {
	return nil, errors.New("unexpected source batch read")
}

func (discardReleaseScope) BoundSources(context.Context, string, payloadreleasemodel.Scope, int) ([]payloadreleasemodel.Scope, error) {
	return nil, nil
}
