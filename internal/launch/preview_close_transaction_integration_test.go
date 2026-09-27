//go:build integration

package launch

import (
	"context"
	"errors"
	"reflect"
	"testing"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

type failPreviewCloseAfterWork struct {
	repository application.PreviewCloseRepository
	failure    error
}

func (r failPreviewCloseAfterWork) WithPreviewClose(ctx context.Context, work func(application.PreviewCloseScope) error) error {
	return r.repository.WithPreviewClose(ctx, func(scope application.PreviewCloseScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return r.failure
	})
}

func TestPreviewCloseRollsBackSessionAndIsolationTogether(t *testing.T) {
	fixture, created := newPlaySourceFixture(t, true, true)
	seedPlayCapability(t, fixture, created.LaunchID, true)
	before := playRows(t, fixture.database)
	cause := errors.New("late preview close failure")
	repository := failPreviewCloseAfterWork{repository: persistence.NewPreviewClose(fixture.database), failure: cause}
	closer := application.NewPreviewCloser(repository, fixture.launcher.now, retromruntime.MatchesCapability)
	err := closer.Finish(t.Context(), created.LaunchID, created.Capability)
	if !errors.Is(err, cause) || !reflect.DeepEqual(before, playRows(t, fixture.database)) {
		t.Fatalf("preview partially closed: %v", err)
	}
	if err := fixture.launcher.AuthorizeSave(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatalf("rollback lost preview save access: %v", err)
	}
}
