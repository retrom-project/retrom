//go:build integration

package launch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testsupport"
)

func TestRPGProductLaunchUsesCurrentBundleAfterProviderUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGReviewFixture(t, database.SQL, now.UnixMilli())
	mustRPGLaunchSQL(t, database.SQL, `UPDATE review_drafts SET metadata_json='{"title":"RPG upgrade"}' WHERE import_item_id=?`, fixture.itemID)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	launcher := newRPGReviewLaunchService(t, ctx, database.SQL, credentials, func() time.Time { return now })
	approved, err := libraryimport.New(database.SQL, func() time.Time { return now }).Approve(ctx, fixture.itemID, 2)
	if err != nil {
		t.Fatalf("approve RPG review: %v", err)
	}

	upgradedBundle := strings.Repeat("b", 64)
	mustRPGLaunchSQL(t, database.SQL, `
UPDATE runtime_providers SET provider_version='1.1.0',bundle_sha256=?,activated_at_ms=activated_at_ms+1
WHERE provider_id='retrom-runtime'
`, upgradedBundle)
	launcher = newRPGReviewLaunchService(t, ctx, database.SQL, credentials, func() time.Time { return now.Add(time.Second) })
	created, err := launcher.Create(ctx, "local", CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID,
	})
	if err != nil || created.LaunchID == "" {
		t.Fatalf("create RPG product after Provider upgrade = %#v, error=%v", created, err)
	}
	configuration, err := launcher.Config(ctx, created.LaunchID, created.Capability)
	if err != nil {
		t.Fatalf("RPG product config after Provider upgrade: %v", err)
	}
	runtimeIdentity := testsupport.RuntimeEnvelopeObject(t, testsupport.RuntimeEnvelope(t, configuration), "runtime")
	if runtimeIdentity["bundleSha256"] != upgradedBundle {
		t.Fatalf("RPG product bundle = %#v, want %s", runtimeIdentity["bundleSha256"], upgradedBundle)
	}
}

func TestRPGProjectContentUsesOnlyUniqueASCIICaseFoldFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGReviewFixture(t, database.SQL, now.UnixMilli())
	credentials, err := retromruntime.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newRPGReviewLaunchService(t, ctx, database.SQL, credentials, func() time.Time { return now })
	mustRPGLaunchSQL(t, database.SQL, `UPDATE review_drafts SET metadata_json='{"title":"RPG content"}' WHERE import_item_id=?`, fixture.itemID)
	published, err := libraryimport.New(database.SQL, func() time.Time { return now }).Approve(ctx, fixture.itemID, 2)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Create(ctx, "local", CreateRequest{GameID: published.GameID, ReturnTo: "/games/" + published.GameID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Config(ctx, created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}

	exact, err := service.RPGProjectContentAuthorized(ctx, created.LaunchID, "RPG_RT.ldb")
	if err != nil || exact.Digest != fixture.projectSHA {
		t.Fatalf("exact RPG content = %#v, error=%v", exact, err)
	}
	folded, err := service.RPGProjectContentAuthorized(ctx, created.LaunchID, "rpg_rt.LDB")
	if err != nil || folded.Digest != fixture.projectSHA {
		t.Fatalf("folded RPG content = %#v, error=%v", folded, err)
	}
	if _, err := service.ContentAuthorized(ctx, created.LaunchID, "rpg_rt.LDB"); !errors.Is(err, ErrCredential) {
		t.Fatalf("ordinary content accepted folded path: %v", err)
	}

	mustRPGLaunchSQL(t, database.SQL, `
	INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
	SELECT launch_session_id,'rpg_rt.ldb',blob_id,format_version,created_at_ms
FROM launch_content_files
WHERE launch_session_id=? AND logical_name='RPG_RT.ldb'`, created.LaunchID)
	if _, err := service.RPGProjectContentAuthorized(ctx, created.LaunchID, "RpG_Rt.LdB"); !errors.Is(err, ErrCredential) {
		t.Fatalf("ambiguous folded RPG content accepted: %v", err)
	}
}
