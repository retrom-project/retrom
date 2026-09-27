package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"retrom/internal/service/cleanupjobs"
)

type discardReleaseRepository struct {
	*discardFixture
	cause error
}

func (fixture discardReleaseRepository) WithDiscard(_ context.Context, work func(ReviewDiscardScope) error) error {
	return work(ReviewDiscardScope{
		Reader: fixture.discardFixture, Tags: fixture.discardFixture, Writer: fixture.discardFixture,
		Payload: failedReviewScheduling{cause: fixture.cause},
	})
}

type failedReviewScheduling struct {
	cleanupjobs.ItemSchedulingScope
	cause error
}

func (scope failedReviewScheduling) Owner(context.Context, cleanupjobs.Scope) (cleanupjobs.Owner, error) {
	return cleanupjobs.Owner{}, scope.cause
}

func TestReviewDiscardUsesTypedPayloadScopeAndPreservesCause(t *testing.T) {
	fixture := newDiscardFixture()
	cause := errors.New("release owner read failed")
	service := discardService(fixture)
	service.repository = discardReleaseRepository{discardFixture: fixture, cause: cause}
	result, err := service.Discard(t.Context(), discardRequest())
	if !errors.Is(err, cause) || result != (ReviewDecisionResult{}) {
		t.Fatalf("typed payload failure result=%+v error=%v", result, err)
	}
	if !reflect.DeepEqual(fixture.steps, []string{"attachments", "item", "owner"}) {
		t.Fatalf("writes before payload read=%v", fixture.steps)
	}
}

func (fixture *discardFixture) releaseScope() cleanupjobs.ItemSchedulingScope {
	scope := discardReleaseScope{fixture: fixture}
	return scope
}

type discardReleaseScope struct{ fixture *discardFixture }

func (scope discardReleaseScope) Owner(_ context.Context, owner cleanupjobs.Scope) (cleanupjobs.Owner, error) {
	state := "COMPLETED"
	if owner.Type == cleanupjobs.ScopeImportItem {
		if err := scope.fixture.step("payload"); err != nil {
			return cleanupjobs.Owner{}, err
		}
		state = "DISCARDED"
	}
	return cleanupjobs.Owner{Scope: owner, State: state, PayloadState: "RELEASED", Version: 1, ReleaseJobID: "release"}, nil
}
func (discardReleaseScope) PendingChildren(context.Context, string) (int64, error) { return 0, nil }
func (discardReleaseScope) CreateJob(context.Context, cleanupjobs.ScheduledJob) error {
	return errors.New("unexpected release job creation")
}

func (discardReleaseScope) BeginRelease(context.Context, cleanupjobs.OwnerRelease) error {
	return errors.New("unexpected release projection")
}
