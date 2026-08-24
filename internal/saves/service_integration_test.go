//go:build integration

package saves

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type saveFixture struct {
	ctx         context.Context
	database    *store.DB
	blobs       *blobstore.Store
	launches    *launch.Service
	saves       *Service
	gameID      string
	now         *time.Time
	credentials *retromruntime.Credentials
}

func newSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.Date(2026, time.August, 6, 2, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), clock)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, clock()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	content, err := blobs.Put(bytes.NewReader([]byte("save-fixture-gba")))
	testassert.False(t, err != nil, err)
	contentBlobID, err := blobstore.EnsureRecord(
		ctx,
		database.SQL,
		content,
		"application/octet-stream",
		clock().UnixMilli(),
	)
	testassert.False(t, err != nil, err)
	var artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id='mgba'
AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	gameID := uuid.NewString()
	metadataID := uuid.NewString()
	contentID := uuid.NewString()
	variantID := uuid.NewString()
	variantRevisionID := uuid.NewString()
	dependencySnapshot, status, _, err := corevalidation.ResolveBIOS(ctx, database.SQL, artifactID, "save.gba")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return status != "READY" }), "save fixture dependencies = %#v/%s, error=%v", dependencySnapshot, status, err)
	validationDigest, err := corevalidation.ValidationInputDigest(
		artifactID,
		contentID,
		sql.NullString{},
		dependencySnapshot,
	)
	testassert.False(t, err != nil, err)
	dependencySnapshotJSON, err := dependencySnapshot.JSON()
	testassert.False(t, err != nil, err)
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
PRAGMA defer_foreign_keys=ON
`); err != nil {
		t.Fatal(err)
	}
	stamp := clock().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{
			`
INSERT INTO game_metadata_revisions(id,
game_id,
title,
title_initial,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
'Save Fixture',
'S',
'',
'',
'',
'',
NULL,
NULL,
'ADMIN_EDIT',
NULL,
?)
`,
			[]any{metadataID, gameID, stamp},
		},
		{
			`
INSERT INTO game_content_revisions(id,
game_id,
source_kind,
source_ref_id,
source_manifest_json,
source_manifest_digest,
created_at_ms) VALUES(?,
?,
'ADMIN_REPLACE',
'fixture',
'{}',
?,
?)
`,
			[]any{contentID, gameID, strings.Repeat("1", 64), stamp},
		},
		{
			`
INSERT INTO games(id,
platform_instance_id,
status,
current_metadata_revision_id,
current_content_revision_id,
search_text,
version,
created_at_ms,
updated_at_ms) VALUES(?,
(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
'PUBLISHED',
?,
?,
'save fixture',
1,
?,
?)
`,
			[]any{gameID, metadataID, contentID, stamp, stamp},
		},
		{
			`
INSERT INTO game_content_files(game_content_revision_id,
role,
logical_name,
blob_id,
source_archive_blob_id,
source_archive_entry_ordinal,
sort_order) VALUES(?,
'CONTENT',
'save.gba',
?,
NULL,
NULL,
0)
`,
			[]any{contentID, contentBlobID},
		},
		{
			`
INSERT INTO game_variants(id,
game_id,
core_id,
current_revision_id,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
'mgba',
NULL,
1,
?,
?)
`,
			[]any{variantID, gameID, stamp, stamp},
		},
		{
			`
INSERT INTO game_variant_revisions(id,
game_variant_id,
game_content_revision_id,
core_artifact_id,
dat_version_id,
validation_input_digest,
emulator_game_id,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
?,
?,
NULL,
?,
424242,
'READY',
'READY',
?,
?)
`,
			[]any{
				variantRevisionID,
				variantID,
				contentID,
				artifactID,
				validationDigest,
				string(dependencySnapshotJSON),
				stamp,
			},
		},
		{`
UPDATE game_variants
SET current_revision_id=?
WHERE id=?
`, []any{variantRevisionID, variantID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed save fixture: %v", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	launcher := launch.New(database.SQL, dependencySet, credentials, clock)
	return &saveFixture{
		ctx:         ctx,
		database:    database,
		blobs:       blobs,
		launches:    launcher,
		saves:       New(database.SQL, blobs, credentials, clock),
		gameID:      gameID,
		now:         &now,
		credentials: credentials,
	}
}

func (fixture *saveFixture) createLaunch(t *testing.T) launch.Created {
	t.Helper()
	created, err := fixture.launches.Create(fixture.ctx, "local", launch.CreateRequest{
		GameID: fixture.gameID, ReturnTo: "/games/" + fixture.gameID,
		ClientCapabilities: launch.Capabilities{
			SecureContext:       true,
			CrossOriginIsolated: true,
			SharedArrayBuffer:   true,
		},
	})
	testassert.False(t, err != nil, err)
	if _, err := fixture.launches.Config(fixture.ctx, created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}
	return created
}

func manualRequest(t *testing.T, name string, state, screenshot []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadataHeader := makeTextHeader("metadata", "metadata.json", "application/json")
	metadata, err := writer.CreatePart(metadataHeader)
	testassert.False(t, err != nil, err)
	_, _ = metadata.Write([]byte(`{"name":"` + name + `"}`))
	statePart, err := writer.CreateFormFile("state", "state.bin")
	testassert.False(t, err != nil, err)
	_, _ = statePart.Write(state)
	screenshotHeader := makeTextHeader("screenshot", "screenshot.png", "image/png")
	screenshotPart, err := writer.CreatePart(screenshotHeader)
	testassert.False(t, err != nil, err)
	_, _ = screenshotPart.Write(screenshot)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", &body)
	testassert.False(t, err != nil, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func makeTextHeader(name, filename, mediaType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="` + name + `"; filename="` + filename + `"`},
		"Content-Type":        {mediaType},
	}
}

func screenshotPNG(t *testing.T) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 2, 2))
	value.Set(0, 0, color.RGBA{R: 0x68, G: 0x55, B: 0xd9, A: 0xff})
	var result bytes.Buffer
	if err := png.Encode(&result, value); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestManualStateRequiresAtomicNonEmptyStateAndScreenshot(t *testing.T) {
	fixture := newSaveFixture(t)
	created := fixture.createLaunch(t)
	key := uuid.NewString()
	state := []byte("manual-state")
	screenshot := screenshotPNG(t)
	result, replayed, err := fixture.saves.CreateManual(
		fixture.ctx,
		created.LaunchID,
		created.Capability,
		key,
		manualRequest(t, "存档一", state, screenshot),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayed }, func() bool { return result.SaveStateID == "" }), "manual state = %#v, replayed=%v, error=%v", result, replayed, err)
	var sourceLaunchID string
	var stateSize, screenshotSize int64
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT s.source_launch_session_id,
state_blob.size_bytes,
screenshot_blob.size_bytes
FROM save_states s
JOIN blobs state_blob ON state_blob.id=s.state_blob_id
JOIN blobs screenshot_blob ON screenshot_blob.id=s.screenshot_blob_id
WHERE s.id=?
`, result.SaveStateID).Scan(&sourceLaunchID, &stateSize, &screenshotSize); err != nil ||
		sourceLaunchID != created.LaunchID ||
		stateSize != int64(len(state)) ||
		screenshotSize != int64(len(screenshot)) {
		t.Fatalf("manual source/blob references = %s/%d/%d, error=%v", sourceLaunchID, stateSize, screenshotSize, err)
	}
	replay, replayed, err := fixture.saves.CreateManual(
		fixture.ctx,
		created.LaunchID,
		created.Capability,
		key,
		manualRequest(t, "存档一", state, screenshot),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !replayed }, func() bool { return replay.SaveStateID != result.SaveStateID }), "manual replay = %#v, replayed=%v, error=%v", replay, replayed, err)
	if _, _, err := fixture.saves.CreateManual(fixture.ctx, created.LaunchID, created.Capability, uuid.NewString(), manualRequest(t, "空状态", nil, screenshot)); !errors.Is(
		err,
		ErrInvalid,
	) {
		t.Fatalf("empty state error = %v", err)
	}
	var count int
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT count(*)
FROM save_states
`).Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("save count after invalid request = %d, error=%v", count, err)
	}
}
