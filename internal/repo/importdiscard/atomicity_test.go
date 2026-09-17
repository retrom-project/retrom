package importdiscard_test

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/model/importdiscard"
)

func TestDiscardRequestAndAuditRollbackTogether(t *testing.T) {
	f := newFixture(t)
	result := f.create(t, f.file(t, "pending.nes", 43))
	// Force a late audit failure after the disposition has been inserted.
	f.exec(t, `DROP TABLE audit_events`)
	if _, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID); err == nil {
		t.Fatal("missing audit table accepted")
	}
	if f.count(t, `SELECT count(*) FROM import_batch_discards WHERE import_id=?`, result.Created.ImportJobID) != 0 {
		t.Fatal("failed audit left a durable request")
	}
	status, err := f.service.Get(f.ctx, "IMPORT", result.Created.ImportJobID)
	if err != nil || status.State != "AVAILABLE" {
		t.Fatalf("rollback status: %+v %v", status, err)
	}
}

func TestDiscardReadDistinguishesInvalidMissingAndCancelled(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Get(f.ctx, "IMPORT", "not-an-id"); !errors.Is(err, importdiscard.ErrInvalid) {
		t.Fatalf("invalid identity: %v", err)
	}
	const missing = "01980000-0000-7000-8000-000000009981"
	if _, err := f.service.Get(f.ctx, "IMPORT", missing); !errors.Is(err, importdiscard.ErrNotFound) {
		t.Fatalf("missing identity: %v", err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err := f.service.Get(ctx, "IMPORT", missing); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
}
