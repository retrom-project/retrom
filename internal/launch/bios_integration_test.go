//go:build integration

package launch

import (
	"bytes"
	"context"
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
	target, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "melonds")
	if err != nil {
		t.Fatal(err)
	}
	var platformInstanceID string
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM platform_instances WHERE platform_id='nds' AND enabled=1`).Scan(&platformInstanceID); err != nil {
		t.Fatal(err)
	}
	requirements := make([]melondsRequirement, 0, 3)
	rows, err := database.SQL.QueryContext(ctx, `
SELECT id,logical_name,emulator_path,version
FROM bios_requirements
WHERE provider_id=? AND target_id=? AND delivery_kind='EXTERNAL_FILE' AND enabled=1
ORDER BY logical_name
`, target.ProviderID, target.TargetID)
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
	snapshot, status, _, err := corevalidation.ResolveBIOS(
		ctx, database.SQL, target.ProviderID, target.TargetID, "game.nds",
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return status != "READY" }), "MelonDS BIOS snapshot = %#v/%s, error=%v", snapshot, status, err)
	gameID, variantID := newUUID(), newUUID()
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
		{`INSERT INTO games(
id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms)
VALUES(?,?,'MelonDS fixture','M','','','','',NULL,NULL,'IMPORT_REVIEW','fixture','SINGLE_FILE',
'IMPORT_REVIEW','fixture','{}',?,'PUBLISHED','melonds fixture',1,?,?)`, []any{gameID, platformInstanceID, strings.Repeat("a", 64), now, now}},
		{`INSERT INTO game_files(game_id,role,logical_name,blob_id,sort_order) VALUES(?,'CONTENT','game.nds',?,0)`, []any{gameID, gameBlobID}},
		{`INSERT INTO game_variants(
id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,status,compatibility_code,
dependency_snapshot_json,version,created_at_ms,updated_at_ms)
VALUES(?,?,'melonds',?,?,NULL,8100,'READY','READY',?,1,?,?)`, []any{variantID, gameID, target.ProviderID, target.TargetID, string(snapshotJSON), now, now}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	testassert.False(t, err != nil, err)
	service := New(database.SQL, dependencySet, credentials, time.Now).
		WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder)
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
	testassert.False(t, err != nil, err)
	envelope := testsupport.RuntimeEnvelope(t, configuration)
	runtimeIdentity := testsupport.RuntimeEnvelopeObject(t, envelope, "runtime")
	external := testsupport.RuntimeEnvelopeResource(t, envelope, "external")
	externalFiles := testsupport.RuntimeResourceFiles(t, external)
	testassert.Falsef(t, testassert.Any(
		func() bool { return runtimeIdentity["targetId"] != "melonds" },
		func() bool { return len(externalFiles) != 3 },
	), "MelonDS envelope = %#v", envelope)
	byVirtualPath := make(map[string]string, len(externalFiles))
	for _, file := range externalFiles {
		virtualPath, _ := file["virtualPath"].(string)
		url, _ := file["url"].(string)
		byVirtualPath[virtualPath] = url
	}
	for _, item := range requirements {
		digest, blobErr := service.ExternalBlob(ctx, launch.LaunchID, launch.Capability, item.logicalName)
		expectedDigest := item.oldDigest
		if useNew {
			expectedDigest = item.newDigest
		}
		expectedIdentity, identityErr := ExternalContentIdentity(expectedDigest)
		expectedURL, urlErr := RuntimeContentURL("external", expectedIdentity, item.logicalName)
		testassert.CheckFalsef(t, testassert.Any(
			func() bool { return identityErr != nil },
			func() bool { return urlErr != nil },
			func() bool { return byVirtualPath[strings.TrimPrefix(item.virtualPath, "/")] != expectedURL },
		), "external mapping %s = %q", item.virtualPath, byVirtualPath[strings.TrimPrefix(item.virtualPath, "/")])
		testassert.CheckFalsef(t, testassert.Any(func() bool { return blobErr != nil }, func() bool { return digest != expectedDigest }), "external %s = %s, error=%v", item.logicalName, digest, blobErr)
	}
	bundle, err := service.BundleFiles(ctx, launch.LaunchID, launch.Capability, "BIOS_BUNDLE")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(bundle) != 0 }), "external BIOS leaked into bundle = %#v, error=%v", bundle, err)
}
