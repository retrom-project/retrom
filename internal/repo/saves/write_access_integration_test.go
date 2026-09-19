//go:build integration

package saves

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	savesmodel "retrom/internal/model/saves"
)

func TestCheckpointRechecksExpiryAfterReadingBody(t *testing.T) {
	fixture := newSaveFixture(t)
	launch := fixture.createLaunch(t)
	request := manualRequest(t, "late save", []byte("payload"), screenshotPNG(t))
	request.Body = &saveAccessBody{Reader: request.Body, beforeRead: func() {
		*fixture.now = fixture.now.Add(9 * time.Hour)
	}}
	_, _, err := fixture.saves.CreateManual(fixture.ctx, launch.LaunchID, launch.Capability, "late-key", request)
	if !errors.Is(err, savesmodel.ErrCredential) {
		t.Fatalf("expired launch wrote checkpoint after body receive: %v", err)
	}
	var saves, replays int
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT (SELECT count(*) FROM save_states),(SELECT count(*) FROM idempotency_records
WHERE operation_id='postRuntimeSaveState')`).Scan(&saves, &replays); err != nil || saves != 0 || replays != 0 {
		t.Fatalf("expired write left save=%d replay=%d error=%v", saves, replays, err)
	}
}

func TestCheckpointLookupPreservesCancellation(t *testing.T) {
	fixture := newSaveFixture(t)
	launch := fixture.createLaunch(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := fixture.saves.CheckpointStatus(ctx, launch.LaunchID, launch.Capability)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup reported credential failure: %v", err)
	}
}

type saveAccessBody struct {
	io.Reader
	beforeRead func()
}

func (body *saveAccessBody) Read(target []byte) (int, error) {
	if body.beforeRead != nil {
		body.beforeRead()
		body.beforeRead = nil
	}
	return body.Reader.Read(target)
}
