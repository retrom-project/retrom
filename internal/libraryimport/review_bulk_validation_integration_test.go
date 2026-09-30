//go:build integration

package libraryimport

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	librarycomposition "retrom/internal/composition/libraryimport"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func TestBulkApprovalUsesTheSameValidationAsIndividualApproval(t *testing.T) {
	for _, reason := range []string{"missing BIOS", "long title", "control character in title"} {
		t.Run(reason, func(t *testing.T) { assertInvalidBulkApproval(t, reason) })
	}
}

func assertInvalidBulkApproval(t *testing.T, reason string) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	t.Cleanup(fixture.service.Close)
	created := fixture.create(t, "stale-bulk", "bulk validation evidence", 1)
	itemID := created.Items[0].ItemID
	profileID, actorID := uuid.NewString(), uuid.NewString()
	fixture.execute(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Bulk Admin',1)`, profileID)
	fixture.execute(t, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,'Bulk Admin','ADMIN','ENABLED',1,1)`, actorID, profileID, "bulk-"+actorID[:8])
	ctx := authn.WithPrincipal(fixture.ctx, authn.Principal{UserID: actorID, ProfileID: profileID, Role: "ADMIN"})
	var version int64
	if err := dbapi.QueryRowContext(ctx, fixture.database, `SELECT review_version FROM import_items WHERE id=?`, itemID).
		Scan(&version); err != nil {
		t.Fatal(err)
	}
	switch reason {
	case "missing BIOS":
		fixture.execute(t, `UPDATE bios_requirements SET requirement_mode='REQUIRED' WHERE core_id='mgba' AND logical_name='gba_bios.bin'`)
	case "long title", "control character in title":
		title := strings.Repeat("界", 201)
		if reason == "control character in title" {
			title = "Invalid\nTitle"
		}
		fixture.execute(t, `UPDATE import_items SET metadata_json=json_set(metadata_json,'$.title',?) WHERE id=?`,
			title, itemID)
	}
	approvals := fixture.service.approvals
	if _, err := approvals.Approve(ctx, libraryservice.ReviewApprovalRequest{
		ItemID: itemID, ExpectedVersion: version,
	}); !errors.Is(err, libraryservice.ErrInvalid) {
		t.Fatalf("individual approval accepted %s: %v", reason, err)
	}
	bulk := librarycomposition.NewReviewBulk(fixture.database, approvals, time.Now)
	t.Cleanup(bulk.Close)
	task, err := bulk.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := bulk.Get(ctx, task.BulkApprovalID)
		if err != nil {
			t.Fatal(err)
		}
		if state.State == "COMPLETED" {
			if state.ScannedCount != 1 || state.SkippedNotReadyCount != 1 || state.PublishedCount != 0 {
				t.Fatalf("invalid bulk result=%#v", state)
			}
			break
		}
		if state.State == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("bulk did not settle: %#v", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var games int
	var state string
	if err := dbapi.QueryRowContext(ctx, fixture.database,
		`SELECT state,(SELECT count(*) FROM games) FROM import_items WHERE id=?`, itemID).Scan(&state, &games); err != nil {
		t.Fatal(err)
	}
	if games != 0 || state != "REVIEW_PENDING" {
		t.Fatalf("invalid evidence published: games=%d state=%s", games, state)
	}
}
