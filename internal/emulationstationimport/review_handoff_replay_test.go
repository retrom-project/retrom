package emulationstationimport

import (
	"errors"
	"testing"
	"time"
)

func TestESReviewHandoffPreservesReservationAndReplay(t *testing.T) {
	for _, checkpoint := range []string{"reserved", "attached", "metadata"} {
		t.Run(checkpoint, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			_, unit := startLifecycleImport(t, fixture, "", "nes")
			item, ordinary := reserveExecutionReview(t, fixture, unit)
			metadata := prepareCancellationCheckpoint(t, fixture, unit, item, ordinary, checkpoint)
			if err := fixture.service.finalizeReviewHandoff(
				fixture.context,
				unit,
				item,
				ordinary.Created.ImportJobID,
				ordinary.Items[0].ItemID,
				nil,
			); err != nil {
				t.Fatal(err)
			}
			assertRecoveredReviewIdentity(t, fixture, unit, item, ordinary, metadata)
			before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
			if err := fixture.service.finalizeReviewHandoff(
				fixture.context,
				unit,
				item,
				ordinary.Created.ImportJobID,
				ordinary.Items[0].ItemID,
				nil,
			); err != nil {
				t.Fatal(err)
			}
			if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
				t.Fatal("replay changed source, audit or progress")
			}
		})
	}
}

func TestESReviewHandoffRejectsExpiredAuthority(t *testing.T) {
	for _, boundary := range []string{"lease", "deadline"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			_, unit := startLifecycleImport(t, fixture, "", "nes")
			item, ordinary := reserveExecutionReview(t, fixture, unit)
			prepareCancellationCheckpoint(t, fixture, unit, item, ordinary, "attached")
			at := unit.DeadlineAtMS
			if boundary == "lease" {
				if err := fixture.database.QueryRowContext(fixture.context, `SELECT leased_until_ms FROM jobs WHERE id=?`, unit.JobID).Scan(
					&at,
				); err != nil {
					t.Fatal(err)
				}
			}
			*fixture.now = time.UnixMilli(at)
			before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
			err := fixture.service.finalizeReviewHandoff(
				fixture.context,
				unit,
				item,
				ordinary.Created.ImportJobID,
				ordinary.Items[0].ItemID,
				nil,
			)
			if !errors.Is(err, ErrVersionConflict) {
				t.Fatalf("expiry accepted: %v", err)
			}
			if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
				t.Fatal("expired handoff changed projection")
			}
		})
	}
}
