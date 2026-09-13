package emulationstationimport

import (
	"context"
	"errors"
	"testing"
)

type maintenanceFixture struct {
	recoverErr, expiryErr error
	expired               int
}

func (fixture *maintenanceFixture) Recover(context.Context) error { return fixture.recoverErr }
func (fixture *maintenanceFixture) Expire(context.Context) error {
	fixture.expired++
	return fixture.expiryErr
}

func TestWorkerMaintenancePreservesBothOperationCauses(t *testing.T) {
	cause := errors.New("maintenance write failed")
	fixture := &maintenanceFixture{recoverErr: cause}
	service := NewMaintenance(fixture, fixture)
	if err := service.Maintain(t.Context()); !errors.Is(err, cause) || fixture.expired != 0 {
		t.Fatalf("recover=%v expired=%d", err, fixture.expired)
	}
	fixture.recoverErr, fixture.expiryErr = nil, cause
	if err := service.Maintain(t.Context()); !errors.Is(err, cause) || fixture.expired != 1 {
		t.Fatalf("expiry=%v expired=%d", err, fixture.expired)
	}
}
