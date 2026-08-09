//go:build integration

package saves

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, clock()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	content, err := blobs.Put(bytes.NewReader([]byte("save-fixture-gba")))
	if err != nil {
		t.Fatal(err)
	}
	contentBlobID, err := blobstore.EnsureRecord(
		ctx,
		database.SQL,
		content,
		"application/octet-stream",
		clock().UnixMilli(),
	)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil || status != "READY" {
		t.Fatalf("save fixture dependencies = %#v/%s, error=%v", dependencySnapshot, status, err)
	}
	validationDigest, err := corevalidation.ValidationInputDigest(
		artifactID,
		contentID,
		sql.NullString{},
		dependencySnapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	dependencySnapshotJSON, err := dependencySnapshot.JSON()
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
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
'01980000-0000-7000-8000-000000000005',
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
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	_, _ = metadata.Write([]byte(`{"name":"` + name + `"}`))
	statePart, err := writer.CreateFormFile("state", "state.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = statePart.Write(state)
	screenshotHeader := makeTextHeader("screenshot", "screenshot.png", "image/png")
	screenshotPart, err := writer.CreatePart(screenshotHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = screenshotPart.Write(screenshot)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", &body)
	if err != nil {
		t.Fatal(err)
	}
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

func contentDigest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
}

func contentDigestHex(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
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
	if err != nil || replayed || result.SaveStateID == "" {
		t.Fatalf("manual state = %#v, replayed=%v, error=%v", result, replayed, err)
	}
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
	if err != nil || !replayed || replay.SaveStateID != result.SaveStateID {
		t.Fatalf("manual replay = %#v, replayed=%v, error=%v", replay, replayed, err)
	}
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

func TestPersistentSaveLocksLaunchBaseAndEnforcesSequence(t *testing.T) {
	fixture := newSaveFixture(t)
	first := fixture.createLaunch(t)
	stale := fixture.createLaunch(t)
	firstBytes := []byte("persistent-one")
	firstKey := uuid.NewString()
	firstResult, replayed, err := fixture.saves.PutPersistent(
		fixture.ctx,
		first.LaunchID,
		first.Capability,
		firstKey,
		contentDigest(firstBytes),
		"AUTO_INTERVAL",
		1,
		bytes.NewReader(firstBytes),
	)
	if err != nil || replayed || firstResult.Sequence != 1 {
		t.Fatalf("first persistent save = %#v, replayed=%v, error=%v", firstResult, replayed, err)
	}
	idempotentReplay, replayed, err := fixture.saves.PutPersistent(
		fixture.ctx,
		first.LaunchID,
		first.Capability,
		firstKey,
		contentDigest(firstBytes),
		"AUTO_INTERVAL",
		1,
		bytes.NewReader(firstBytes),
	)
	if err != nil || !replayed || idempotentReplay.RevisionID != firstResult.RevisionID {
		t.Fatalf("persistent idempotency replay = %#v, replayed=%v, error=%v", idempotentReplay, replayed, err)
	}
	conflicting := []byte("persistent-conflict")
	if _, _, err := fixture.saves.PutPersistent(
		fixture.ctx,
		first.LaunchID,
		first.Capability,
		firstKey,
		contentDigest(conflicting),
		"AUTO_INTERVAL",
		1,
		bytes.NewReader(conflicting),
	); !errors.Is(err, ErrIdempotencyReused) {
		t.Fatalf("persistent idempotency conflict error = %v", err)
	}
	replay, replayed, err := fixture.saves.PutPersistent(
		fixture.ctx,
		first.LaunchID,
		first.Capability,
		uuid.NewString(),
		contentDigest(firstBytes),
		"AUTO_INTERVAL",
		1,
		bytes.NewReader(firstBytes),
	)
	if err != nil || !replayed || replay.RevisionID != firstResult.RevisionID {
		t.Fatalf("persistent replay = %#v, replayed=%v, error=%v", replay, replayed, err)
	}
	changed := []byte("changed")
	if _, _, err := fixture.saves.PutPersistent(fixture.ctx, first.LaunchID, first.Capability, uuid.NewString(), contentDigest(changed), "AUTO_INTERVAL", 1, bytes.NewReader(changed)); !errors.Is(
		err,
		ErrSequenceReused,
	) {
		t.Fatalf("changed replay error = %v", err)
	}
	if _, _, err := fixture.saves.PutPersistent(fixture.ctx, first.LaunchID, first.Capability, uuid.NewString(), contentDigest(changed), "AUTO_INTERVAL", 3, bytes.NewReader(changed)); !errors.Is(
		err,
		ErrSequenceGap,
	) {
		t.Fatalf("sequence gap error = %v", err)
	}
	if _, _, err := fixture.saves.PutPersistent(fixture.ctx, stale.LaunchID, stale.Capability, uuid.NewString(), contentDigest(changed), "AUTO_INTERVAL", 1, bytes.NewReader(changed)); !errors.Is(
		err,
		ErrPersistentConflict,
	) {
		t.Fatalf("stale launch conflict = %v", err)
	}
	current := fixture.createLaunch(t)
	metadata, exists, err := fixture.saves.GetPersistent(fixture.ctx, current.LaunchID, current.Capability)
	if err != nil || !exists || metadata.SHA256 != contentDigestHex(firstBytes) {
		t.Fatalf("locked persistent base = %#v, exists=%v, error=%v", metadata, exists, err)
	}
	secondBytes := []byte("persistent-two")
	second, _, err := fixture.saves.PutPersistent(
		fixture.ctx,
		current.LaunchID,
		current.Capability,
		uuid.NewString(),
		contentDigest(secondBytes),
		"MANUAL_EXPORT",
		1,
		bytes.NewReader(secondBytes),
	)
	if err != nil || second.RevisionID == firstResult.RevisionID {
		t.Fatalf("advanced persistent save = %#v, error=%v", second, err)
	}
	if _, _, err := fixture.saves.PutPersistent(fixture.ctx, first.LaunchID, first.Capability, uuid.NewString(), contentDigest(secondBytes), "EXIT", 2, bytes.NewReader(secondBytes)); !errors.Is(
		err,
		ErrPersistentConflict,
	) {
		t.Fatalf("concurrent launch conflict = %v", err)
	}
	var baseBlobID string
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT blob_id
FROM persistent_save_revisions
WHERE id=?
`, firstResult.RevisionID).Scan(&baseBlobID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(fixture.ctx, `
UPDATE blobs
SET size_bytes=?
WHERE id=?
`, maxStateBytes+1, baseBlobID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.saves.GetPersistent(fixture.ctx, current.LaunchID, current.Capability); !errors.Is(
		err,
		ErrTooLarge,
	) {
		t.Fatalf("oversized locked base error = %v", err)
	}
}

func TestPersistentSaveNoneRejectsGetAndPutWithoutCreatingRows(t *testing.T) {
	fixture := newSaveFixture(t)
	var artifactID, compatibilityJSON string
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT id,compatibility_config_json FROM core_artifacts WHERE core_id='mgba' AND enabled=1
`).Scan(&artifactID, &compatibilityJSON); err != nil {
		t.Fatal(err)
	}
	var compatibility map[string]any
	if err := json.Unmarshal([]byte(compatibilityJSON), &compatibility); err != nil {
		t.Fatal(err)
	}
	compatibility["persistentSaveMode"] = "NONE"
	compatibility["persistentSaveKind"] = nil
	updatedCompatibility, err := json.Marshal(compatibility)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(
		fixture.ctx,
		`UPDATE core_artifacts SET compatibility_config_json=? WHERE id=?`,
		string(updatedCompatibility),
		artifactID,
	); err != nil {
		t.Fatal(err)
	}
	created := fixture.createLaunch(t)
	var persistentBase sql.NullString
	if err := fixture.database.SQL.QueryRowContext(
		fixture.ctx,
		`SELECT persistent_save_base_revision_id FROM launch_sessions WHERE id=?`,
		created.LaunchID,
	).Scan(&persistentBase); err != nil || persistentBase.Valid {
		t.Fatalf("NONE persistent base = %v, error=%v", persistentBase, err)
	}
	if _, _, err := fixture.saves.GetPersistent(
		fixture.ctx,
		created.LaunchID,
		created.Capability,
	); !errors.Is(err, ErrPersistentUnsupported) {
		t.Fatalf("NONE persistent GET error = %v", err)
	}
	if _, _, err := fixture.saves.PutPersistent(
		fixture.ctx,
		created.LaunchID,
		created.Capability,
		uuid.NewString(),
		contentDigest([]byte("unsupported")),
		"AUTO_INTERVAL",
		1,
		bytes.NewReader([]byte("unsupported")),
	); !errors.Is(err, ErrPersistentUnsupported) {
		t.Fatalf("NONE persistent PUT error = %v", err)
	}
	var count int
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `SELECT count(*) FROM persistent_saves`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("NONE persistent rows = %d, error=%v", count, err)
	}
}
