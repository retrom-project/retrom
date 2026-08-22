//go:build integration

package launch

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestMelonDSExternalBIOSIsLockedPerLaunch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	var artifactID, platformInstanceID string
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM core_artifacts WHERE core_id='melonds' AND enabled=1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM platform_instances WHERE platform_id='nds' AND enabled=1`).Scan(&platformInstanceID); err != nil {
		t.Fatal(err)
	}
	requirements := make([]melondsRequirement, 0, 3)
	rows, err := database.SQL.QueryContext(ctx, `
SELECT id,logical_name,emulator_path,version
FROM bios_requirements
WHERE core_artifact_id=? AND delivery_kind='EXTERNAL_FILE' AND enabled=1
ORDER BY logical_name
`, artifactID)
	testassert.False(t, err != nil, err)
	for rows.Next() {
		var item melondsRequirement
		if err := rows.Scan(&item.id, &item.logicalName, &item.virtualPath, &item.version); err != nil {
			t.Fatal(err)
		}
		requirements = append(requirements, item)
	}
	cleanup.Error("close", rows.Close())
	if err := rows.Err(); err != nil || len(requirements) != 3 {
		t.Fatalf("MelonDS requirements = %#v, error=%v", requirements, err)
	}
	install := func(item *melondsRequirement, generation string, active int) string {
		t.Helper()
		metadata, putErr := blobs.Put(bytes.NewReader([]byte(generation + "-" + item.logicalName)))
		testassert.False(t, putErr != nil, putErr)
		blobID, recordErr := blobstore.EnsureRecord(
			ctx,
			database.SQL,
			metadata,
			"application/octet-stream",
			time.Now().UnixMilli(),
		)
		testassert.False(t, recordErr != nil, recordErr)
		installationID, _ := uuid.NewV7()
		if _, execErr := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',?,1,?,?)
`, installationID.String(), item.id, blobID, item.logicalName, metadata.Size, metadata.MD5, metadata.SHA1,
			metadata.SHA256, item.version, active, time.Now().UnixMilli(), time.Now().UnixMilli()); execErr != nil {
			t.Fatal(execErr)
		}
		return metadata.SHA256
	}
	for index := range requirements {
		requirements[index].oldDigest = install(&requirements[index], "old", 1)
	}
	gameMetadata, err := blobs.Put(bytes.NewReader([]byte("nds-content")))
	testassert.False(t, err != nil, err)
	gameBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, gameMetadata, "application/octet-stream", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	snapshot, status, _, err := corevalidation.ResolveBIOS(ctx, database.SQL, artifactID, "game.nds")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return status != "READY" }), "MelonDS BIOS snapshot = %#v/%s, error=%v", snapshot, status, err)
	contentID, gameID, metadataID, variantID, revisionID := newUUID(), newUUID(), newUUID(), newUUID(), newUUID()
	digest, err := corevalidation.ValidationInputDigest(artifactID, contentID, sql.NullString{}, snapshot)
	testassert.False(t, err != nil, err)
	snapshotJSON, err := snapshot.JSON()
	testassert.False(t, err != nil, err)
	now := time.Now().UnixMilli()
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	statements := []struct {
		query string
		args  []any
	}{
		{`PRAGMA defer_foreign_keys=ON`, nil},
		{`INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,created_at_ms)
VALUES(?,?,'MelonDS fixture','','','','',NULL,NULL,'IMPORT_REVIEW','fixture',?)`, []any{metadataID, gameID, now}},
		{`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
VALUES(?,?,'IMPORT_REVIEW','fixture','{}',?,?)`, []any{contentID, gameID, strings.Repeat("a", 64), now}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms)
VALUES(?,?,'PUBLISHED',?,?,'melonds fixture',1,?,?)`, []any{gameID, platformInstanceID, metadataID, contentID, now, now}},
		{`INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order) VALUES(?,'CONTENT','game.nds',?,0)`, []any{contentID, gameBlobID}},
		{`INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms) VALUES(?,?,'melonds',NULL,1,?,?)`, []any{variantID, gameID, now, now}},
		{`INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,NULL,?,8100,'READY','READY',?,?)`, []any{revisionID, variantID, contentID, artifactID, digest, string(snapshotJSON), now}},
		{`UPDATE game_variants SET current_revision_id=? WHERE id=?`, []any{revisionID, variantID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now)
	capabilities := Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	melonds := "melonds"
	oldLaunch, err := service.Create(ctx, "local", CreateRequest{GameID: gameID, CoreID: &melonds, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return oldLaunch.LaunchID == "" }), "old MelonDS launch = %#v, error=%v", oldLaunch, err)
	assertMelonDSLaunch(t, ctx, service, oldLaunch, requirements, false)
	for index := range requirements {
		if _, err := database.SQL.ExecContext(ctx, `UPDATE bios_installations SET is_active=0,version=version+1,updated_at_ms=? WHERE requirement_id=? AND is_active=1`, time.Now().UnixMilli(), requirements[index].id); err != nil {
			t.Fatal(err)
		}
		requirements[index].newDigest = install(&requirements[index], "new", 1)
	}
	assertMelonDSLaunch(t, ctx, service, oldLaunch, requirements, false)
	pending, err := service.Create(ctx, "local", CreateRequest{GameID: gameID, CoreID: &melonds, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending.Status != "VALIDATION_PENDING" }, func() bool { return pending.JobID == "" }), "new BIOS validation = %#v, error=%v", pending, err)
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, pending.JobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "new BIOS validation state = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	newLaunch, err := service.Create(ctx, "local", CreateRequest{GameID: gameID, CoreID: &melonds, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return newLaunch.LaunchID == "" }), "new MelonDS launch = %#v, error=%v", newLaunch, err)
	assertMelonDSLaunch(t, ctx, service, newLaunch, requirements, true)
	if _, err := service.ExternalBlob(ctx, newLaunch.LaunchID, oldLaunch.Capability, requirements[0].logicalName); !errors.Is(err, ErrCredential) {
		t.Fatalf("cross-launch capability error = %v", err)
	}
}

func assertMelonDSLaunch(
	t *testing.T,
	ctx context.Context,
	service *Service,
	launch Created,
	requirements []melondsRequirement,
	useNew bool,
) {
	t.Helper()
	configuration, err := service.Config(ctx, launch.LaunchID, launch.Capability)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return configuration.Core != "melonds" }, func() bool { return configuration.RuntimeCore != "melonds" }, func() bool { return configuration.InputMode != "POINTER" }, func() bool { return len(configuration.ExternalFiles) != 3 }), "MelonDS config = %#v, error=%v", configuration, err)
	for _, item := range requirements {
		expectedURL := "/runtime/launches/" + launch.LaunchID + "/external-files/" + item.logicalName
		testassert.CheckFalsef(t, configuration.ExternalFiles[item.virtualPath] != expectedURL, "external mapping %s = %q", item.virtualPath, configuration.ExternalFiles[item.virtualPath])
		digest, blobErr := service.ExternalBlob(ctx, launch.LaunchID, launch.Capability, item.logicalName)
		expectedDigest := item.oldDigest
		if useNew {
			expectedDigest = item.newDigest
		}
		testassert.CheckFalsef(t, testassert.Any(func() bool { return blobErr != nil }, func() bool { return digest != expectedDigest }), "external %s = %s, error=%v", item.logicalName, digest, blobErr)
	}
	bundle, err := service.BundleFiles(ctx, launch.LaunchID, launch.Capability, "BIOS_BUNDLE")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(bundle) != 0 }), "external BIOS leaked into bundle = %#v, error=%v", bundle, err)
}
