//go:build integration

package launch

import (
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testsupport"
)

func TestRPGMakerUsesOrdinaryReviewPreviewWithoutCreatingAGame(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dataDir := t.TempDir()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "review.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGReviewFixture(t, database.SQL, now().UnixMilli())
	mustRPGLaunchSQL(t, database.SQL, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('rpg-reviewer','local','rpg-reviewer','Reviewer','ADMIN','ENABLED',0,0)`)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	launcher := newRPGReviewLaunchService(t, ctx, database.SQL, credentials, now)
	created, err := launcher.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "rpg-reviewer", IdempotencyKey: "rpg-trial",
	})
	if err != nil || created.PreviewID == "" {
		t.Fatalf("RPG Maker cannot use the ordinary review trial: %+v, %v", created, err)
	}
	var games, previews int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM games),(SELECT count(*) FROM review_preview_sessions WHERE id=?)`,
		created.PreviewID).Scan(&games, &previews); err != nil {
		t.Fatal(err)
	}
	if games != 0 || previews != 1 {
		t.Fatalf("trial created a game or bypassed ordinary preview storage: games=%d previews=%d", games, previews)
	}
	configuration, err := launcher.ReviewPreviewConfig(ctx, created.PreviewID, created.Capability)
	if err != nil {
		t.Fatalf("ordinary RPG preview did not produce a Provider launch envelope: %v", err)
	}
	envelope := testsupport.RuntimeEnvelope(t, configuration)
	if testsupport.RuntimeEnvelopeObject(t, envelope, "session")["purpose"] != "REVIEW_PREVIEW" {
		t.Fatal("RPG trial retained a dedicated runtime-validation purpose")
	}
	for _, logicalName := range []string{"RPG_RT.ldb", "Map0001.lmu", rpgEasyIndexName} {
		content, err := launcher.ReviewPreviewProjectContent(ctx, created.PreviewID, created.Capability, logicalName)
		if err != nil || content.Format != rpgProjectFormat {
			t.Fatalf("ordinary preview cannot serve frozen RPG content %s: %v", logicalName, err)
		}
	}
	started, err := launcher.RecordPlay(ctx, created.PreviewID, created.Capability, "start", PlayEvent{
		ClientSequence: 0, ClientObservedAtMS: now().UnixMilli(),
	})
	if err != nil || started.State != "ACTIVE" || started.PlaySessionID != nil {
		t.Fatalf("ordinary Player cannot start a review trial without play statistics: %+v %v", started, err)
	}
	for range 2 {
		finished, err := launcher.RecordPlay(ctx, created.PreviewID, created.Capability, "finish", PlayEvent{
			ClientSequence: 1, ClientObservedAtMS: now().UnixMilli(),
			PreviousInterval: &Interval{Running: true, Visible: true},
		})
		if err != nil || finished.State != "FINISHED" || finished.PlaySessionID != nil {
			t.Fatalf("ordinary Player cannot idempotently close a review trial: %+v %v", finished, err)
		}
	}
	if _, err := launcher.ReviewPreviewConfig(ctx, created.PreviewID, created.Capability); err == nil {
		t.Fatal("closed trial still authorizes runtime configuration")
	}
}
