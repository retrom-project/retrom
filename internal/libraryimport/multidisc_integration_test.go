//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/saves"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

type multiDiscUploadFile struct {
	path     string
	contents []byte
}

func completeMultiDiscUpload(
	t *testing.T,
	ctx context.Context,
	database *store.DB,
	blobs *blobstore.Store,
	dataDir string,
	sourceType string,
	files []multiDiscUploadFile,
) string {
	t.Helper()
	declarations := make([]uploads.FileDeclaration, 0, len(files))
	for index, file := range files {
		declarations = append(declarations, uploads.FileDeclaration{
			ClientFileID: fmt.Sprintf("file-%d", index), RelativePath: file.path, SizeBytes: int64(len(file.contents)),
		})
	}
	service := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := service.Create(ctx, uploads.CreateRequest{SourceType: sourceType, Files: declarations})
	testassert.False(t, err != nil, err)
	for index, file := range files {
		digest := sha256.Sum256(file.contents)
		if err := service.PutPart(
			ctx,
			upload.ID,
			upload.Files[index].ID,
			0,
			fmt.Sprintf("bytes 0-%d/%d", len(file.contents)-1, len(file.contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(file.contents),
		); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := service.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitParentJob(t, database.SQL, jobID, "SUCCEEDED")
	return upload.ID
}

func completeMultiDiscDirectory(
	t *testing.T,
	ctx context.Context,
	database *store.DB,
	blobs *blobstore.Store,
	dataDir string,
	files []multiDiscUploadFile,
) string {
	t.Helper()
	return completeMultiDiscUpload(t, ctx, database, blobs, dataDir, "DIRECTORY", files)
}

func newMultiDiscImportFixture(t *testing.T) (context.Context, string, *store.DB, *blobstore.Store, *Service) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('multi-disc-profile','Multi Disc Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000009991','multi-disc-profile','multi-disc-admin',
'Multi Disc Admin','ADMIN','ENABLED',0,0);
`); err != nil {
		t.Fatal(err)
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{
		UserID: "01980000-0000-7000-8000-000000009991", ProfileID: "multi-disc-profile",
		Username: "multi-disc-admin", DisplayName: "Multi Disc Admin", Role: "ADMIN",
	})
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	bios, err := blobs.Put(bytes.NewReader([]byte("deterministic invalid Saturn BIOS fixture")))
	testassert.False(t, err != nil, err)
	biosBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, bios, "application/octet-stream", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	var requirementID string
	var requirementVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,version FROM bios_requirements
WHERE core_id='yabause' AND logical_name='saturn_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	installationID := "01990000-0000-7000-8000-" + requirementID[len(requirementID)-12:]
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, installationID, requirementID, biosBlobID, "saturn_bios.bin", bios.Size, bios.MD5, bios.SHA1, bios.SHA256,
		requirementVersion, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	importer := New(database.SQL, time.Now).WithBlobStore(blobs).WithMultiDiscImportEnabled(true)
	return ctx, dataDir, database, blobs, importer
}

func fakeCHD(payload string) []byte {
	return append([]byte("MComprHD"), []byte(payload)...)
}

func multiDiscSaveRequest(t *testing.T, discIndex int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="metadata"; filename="metadata.json"`},
		"Content-Type":        {"application/json"},
	})
	testassert.False(t, err != nil, err)
	_, _ = fmt.Fprintf(metadata, `{"name":"跨盘存档","discIndex":%d}`, discIndex)
	state, err := writer.CreateFormFile("state", "state.bin")
	testassert.False(t, err != nil, err)
	_, _ = state.Write([]byte("multi-disc-state"))
	screenshot, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="screenshot"; filename="screenshot.png"`},
		"Content-Type":        {"image/png"},
	})
	testassert.False(t, err != nil, err)
	pngFixture, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFElEQVR4nGP4z8DAwMDAxMDAwMAAAAwBAQDJ/pLvAAAAAElFTkSuQmCC",
	)
	testassert.False(t, err != nil, err)
	_, _ = screenshot.Write(pngFixture)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", &body)
	testassert.False(t, err != nil, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	uploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "Alpha/original.m3u", contents: []byte("Disc One.CHD\nDisc Two.chd\n")},
		{path: "Alpha/disc one.chd", contents: fakeCHD("alpha-one")},
		{path: "Alpha/Disc Two.chd", contents: fakeCHD("alpha-two")},
		{path: "Alpha/notes.txt", contents: []byte("ignored")},
		{path: "Alpha/unreferenced.chd", contents: []byte("tiny")},
		{path: "Nested/Beta/game.m3u", contents: []byte("one.chd\ntwo.chd\n")},
		{path: "Nested/Beta/one.chd", contents: fakeCHD("beta-one")},
		{path: "Nested/Beta/two.chd", contents: fakeCHD("beta-two")},
	})
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.ItemCount != 2 }), "Create() = %#v, error=%v", created, err)
	var importEventData string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT data_json FROM job_events
WHERE job_id=? AND scope_type='IMPORT_GROUP' AND event_type='SUCCEEDED'
`, created.JobID).Scan(&importEventData); err != nil ||
		!strings.Contains(importEventData, `"contentMode":"MULTI_DISC_M3U_V1"`) ||
		!strings.Contains(importEventData, `"parserResultCode":"MATCHED"`) {
		t.Fatalf("multi-disc import event = %q, error=%v", importEventData, err)
	}
	items := queryAttachmentStrings(t, database.SQL, `
SELECT item.id||':'||snapshot.content_kind||':'||validation.prepublish_generation
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE item.import_job_id=? ORDER BY item.group_key
`, created.ImportJobID)
	testassert.Falsef(t, len(items) != 2, "items = %#v", items)
	var firstItemID, firstSnapshotID string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT item.id,snapshot.id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE item.import_job_id=?
AND EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=snapshot.id
  AND file.role='PLAYLIST_SOURCE'
  AND file.logical_name='original.m3u'
)
`, created.ImportJobID).Scan(&firstItemID, &firstSnapshotID); err != nil {
		t.Fatal(err)
	}
	entries := queryAttachmentStrings(t, database.SQL, `
SELECT printf('%d:%s:%s:%s',ordinal,state,normalized_reference,canonical_name)
FROM import_item_multidisc_entries WHERE source_snapshot_id=? ORDER BY ordinal
`, firstSnapshotID)
	testassert.Falsef(t, fmt.Sprint(entries) != "[0:PRESENT:disc one.chd:disc-001.chd 1:PRESENT:disc two.chd:disc-002.chd]", "entries = %v", entries)
	var playlistSHA string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT blob.sha256
FROM import_item_core_validations validation
JOIN import_item_validation_files file ON file.import_item_core_validation_id=validation.id
JOIN blobs blob ON blob.id=file.blob_id
WHERE validation.source_snapshot_id=? AND file.role='MULTI_DISC_PLAYLIST'
`, firstSnapshotID).Scan(&playlistSHA); err != nil {
		t.Fatal(err)
	}
	reader, err := blobs.OpenDigest(playlistSHA)
	testassert.False(t, err != nil, err)
	canonical, err := io.ReadAll(reader)
	cleanup.Error("close", reader.Close())
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return string(canonical) != "disc-001.chd\ndisc-002.chd\n" }), "canonical playlist = %q, error=%v", canonical, err)
	var ignored int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM import_job_files WHERE import_job_id=?
AND disposition='IGNORED' AND reason_code='NOT_REFERENCED_BY_PLAYLIST'
	`, created.ImportJobID).Scan(&ignored); err != nil || ignored != 2 {
		t.Fatalf("ignored files = %d, error=%v", ignored, err)
	}
	approved, err := importer.Approve(ctx, firstItemID, 1)
	testassert.False(t, err != nil, err)
	published := queryAttachmentStrings(t, database.SQL, `
SELECT revision.content_kind||':'||file.role||':'||printf('%d',file.sort_order)
FROM games game
JOIN game_content_revisions revision ON revision.id=game.current_content_revision_id
JOIN game_content_files file ON file.game_content_revision_id=revision.id
WHERE game.id=? ORDER BY file.role,file.sort_order
`, approved.GameID)
	testassert.Falsef(t, len(published) != 3, "published content = %v", published)
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	launcher := launch.New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs)
	createdLaunch, err := launcher.Create(ctx, "multi-disc-profile", launch.CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: launch.Capabilities{
			SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
		},
	})
	testassert.False(t, err != nil, err)
	configuration, err := launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return configuration.DiscSet == nil }, func() bool { return configuration.DiscSet.Count != 2 }, func() bool { return configuration.DiscSet.InitialDiscIndex != 0 }, func() bool {
		return configuration.GameURL != "/runtime/launches/"+createdLaunch.LaunchID+"/game/playlist.m3u"
	}), "multi-disc launch config = %#v, error=%v", configuration, err)
	dimensions, err := launcher.MultiDiscTelemetryDimensions(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return dimensions.PlatformKey != "saturn" }, func() bool { return dimensions.CoreKey != "yabause" }, func() bool { return dimensions.ArtifactVersion < 1 }, func() bool { return dimensions.DiscCount != 2 }), "multi-disc telemetry dimensions = %#v, error=%v", dimensions, err)
	for index, entry := range configuration.DiscSet.Entries {
		expectedName := fmt.Sprintf("disc-%03d.chd", index+1)
		testassert.Falsef(t, testassert.Any(func() bool { return entry.Index != index }, func() bool { return entry.VirtualPath != "/"+expectedName }, func() bool {
			return configuration.ExternalFiles[entry.VirtualPath] !=
				"/runtime/launches/"+createdLaunch.LaunchID+"/external-files/"+expectedName
		}), "disc entry %d = %#v / %#v", index, entry, configuration.ExternalFiles)
		if _, err := launcher.ExternalBlob(ctx, createdLaunch.LaunchID, createdLaunch.Capability, expectedName); err != nil {
			t.Fatalf("locked disc %d: %v", index, err)
		}
		view, err := launcher.External(ctx, createdLaunch.LaunchID, createdLaunch.Capability, expectedName)
		testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return view.Kind != "DISC" }, func() bool { return view.PlatformKey != "saturn" }, func() bool { return view.CoreKey != "yabause" }, func() bool { return view.DiscCount != 2 }, func() bool { return view.ArtifactVersion != dimensions.ArtifactVersion }), "observable disc %d = %#v, error=%v", index, view, err)
	}
	if _, err := launcher.ExternalBlob(
		ctx, createdLaunch.LaunchID, createdLaunch.Capability, "Disc One.CHD",
	); !errors.Is(err, launch.ErrCredential) {
		t.Fatalf("original disc name error = %v", err)
	}
	saveService := saves.New(database.SQL, blobs, credentials, time.Now)
	saved, replayed, err := saveService.CreateManual(
		ctx, createdLaunch.LaunchID, createdLaunch.Capability, "multi-disc-save-1",
		multiDiscSaveRequest(t, 1),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayed }, func() bool { return saved.DiscIndex == nil }, func() bool { return *saved.DiscIndex != 1 }), "multi-disc save = %#v replayed=%t error=%v", saved, replayed, err)
	restoredLaunch, err := launcher.Create(ctx, "multi-disc-profile", launch.CreateRequest{
		GameID: approved.GameID, SaveStateID: &saved.SaveStateID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: launch.Capabilities{
			SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
		},
	})
	testassert.False(t, err != nil, err)
	restoredConfig, err := launcher.Config(ctx, restoredLaunch.LaunchID, restoredLaunch.Capability)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return restoredConfig.DiscSet == nil }, func() bool { return restoredConfig.DiscSet.InitialDiscIndex != 1 }), "restored multi-disc config = %#v, error=%v", restoredConfig, err)
}

func TestMultiDiscMissingDiscIsBlockedWithoutPlaceholderBlob(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	uploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\nthree.chd\n")},
		{path: "game/one.chd", contents: fakeCHD("one")},
		{path: "game/two.chd", contents: fakeCHD("two")},
	})
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.ItemCount != 1 }), "Create() = %#v, error=%v", created, err)
	var itemID, validationStatus, compatibilityCode string
	var selectedValidationID *string
	var missingBlobID, missingUploadID *string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT item.id,validation.status,validation.compatibility_code,draft.selected_validation_id,
entry.blob_id,entry.upload_file_id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN import_item_core_validations validation ON validation.source_snapshot_id=snapshot.id
JOIN import_item_multidisc_entries entry ON entry.source_snapshot_id=snapshot.id AND entry.state='MISSING'
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(
		&itemID, &validationStatus, &compatibilityCode, &selectedValidationID, &missingBlobID, &missingUploadID,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return validationStatus != "BLOCKED" }, func() bool { return compatibilityCode != "MULTI_DISC_FILE_MISSING" }, func() bool { return selectedValidationID != nil }, func() bool { return missingBlobID != nil }, func() bool { return missingUploadID != nil }), "blocked item = %s/%s selected=%v blob=%v upload=%v", validationStatus, compatibilityCode, selectedValidationID, missingBlobID, missingUploadID)
	if _, err := importer.Approve(ctx, itemID, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blocked approve error = %v", err)
	}
	baseSnapshotID := ""
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT effective_source_snapshot_id FROM review_drafts WHERE import_item_id=?
	`, itemID).Scan(&baseSnapshotID); err != nil {
		t.Fatal(err)
	}
	initialReview, hasMultiDisc, err := importer.ReviewMultiDisc(ctx, itemID)
	initialProjection, projectionOK := initialReview.(map[string]any)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !hasMultiDisc }, func() bool { return !projectionOK }, func() bool { return initialProjection["discCount"] != 3 }, func() bool { return initialProjection["presentDiscCount"] != 2 }, func() bool { return initialProjection["missingDiscCount"] != 1 }, func() bool { return initialProjection["canAttachMissingDiscs"] != true }), "initial multi-disc review = %#v, present=%v, error=%v", initialReview, hasMultiDisc, err)
	encodedReview, _ := json.Marshal(initialReview)
	testassert.Falsef(t, testassert.Any(func() bool { return bytes.Contains(encodedReview, []byte("blobId")) }, func() bool { return !bytes.Contains(encodedReview, []byte("playlist")) }), "initial review leaked storage identity or omitted playlist: %s", encodedReview)
	attachmentUploadID := completeMultiDiscUpload(
		t, ctx, database, blobs, dataDir, "FILES",
		[]multiDiscUploadFile{{path: "three.chd", contents: fakeCHD("three")}},
	)
	attachment, err := importer.CreateMultiDiscAttachment(ctx, itemID, 1, MultiDiscAttachmentRequest{
		UploadID: attachmentUploadID,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return attachment.State != "QUEUED" }, func() bool { return attachment.ReviewVersion != 2 }), "CreateMultiDiscAttachment() = %#v, error=%v", attachment, err)
	waitParentJob(t, database.SQL, attachment.JobID, "SUCCEEDED")
	var terminalEventData string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT data_json FROM job_events WHERE job_id=? AND event_type='SUCCEEDED'
ORDER BY id DESC LIMIT 1
`, attachment.JobID).Scan(&terminalEventData); err != nil ||
		!strings.Contains(terminalEventData, `"state":"ACCEPTED"`) ||
		!strings.Contains(terminalEventData, `"validationStatus":"READY"`) ||
		!strings.Contains(terminalEventData, `"durationMs":`) {
		t.Fatalf("attachment terminal event = %q, error=%v", terminalEventData, err)
	}
	var resultSnapshotID, selectedID string
	var version int64
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT effective_source_snapshot_id,selected_validation_id,version
FROM review_drafts WHERE import_item_id=?
`, itemID).Scan(&resultSnapshotID, &selectedID, &version); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return resultSnapshotID == baseSnapshotID }, func() bool { return selectedID == "" }, func() bool { return version != 3 }), "accepted draft snapshot=%s selected=%s version=%d", resultSnapshotID, selectedID, version)
	oldEntries := queryAttachmentStrings(t, database.SQL, `
SELECT state FROM import_item_multidisc_entries WHERE source_snapshot_id=? ORDER BY ordinal
`, baseSnapshotID)
	newEntries := queryAttachmentStrings(t, database.SQL, `
SELECT state FROM import_item_multidisc_entries WHERE source_snapshot_id=? ORDER BY ordinal
`, resultSnapshotID)
	testassert.Falsef(t, testassert.Any(func() bool { return fmt.Sprint(oldEntries) != "[PRESENT PRESENT MISSING]" }, func() bool { return fmt.Sprint(newEntries) != "[PRESENT PRESENT PRESENT]" }), "old/new entries = %v / %v", oldEntries, newEntries)
	var requestedBy, eventActor, attachmentState string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT attachment.requested_by_user_id,attachment.state,event.actor_user_id
FROM review_multidisc_attachments attachment
JOIN review_events event ON event.import_item_id=attachment.import_item_id
AND event.event_type='DISC_ATTACHMENT_ACCEPTED'
WHERE attachment.id=?
`, attachment.AttachmentID).Scan(&requestedBy, &attachmentState, &eventActor); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return requestedBy != "01980000-0000-7000-8000-000000009991" }, func() bool { return eventActor != requestedBy }, func() bool { return attachmentState != "ACCEPTED" }), "attachment actor/state = %s/%s/%s", requestedBy, eventActor, attachmentState)
	acceptedReview, hasMultiDisc, err := importer.ReviewMultiDisc(ctx, itemID)
	acceptedProjection, projectionOK := acceptedReview.(map[string]any)
	latest, latestOK := acceptedProjection["latestAttachment"].(map[string]any)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !hasMultiDisc }, func() bool { return !projectionOK }, func() bool { return !latestOK }, func() bool { return latest["state"] != "ACCEPTED" }, func() bool { return acceptedProjection["presentDiscCount"] != 3 }, func() bool { return acceptedProjection["missingDiscCount"] != 0 }, func() bool { return acceptedProjection["canAttachMissingDiscs"] != false }), "accepted multi-disc review = %#v, present=%v, error=%v", acceptedReview, hasMultiDisc, err)
	if _, err := importer.Approve(ctx, itemID, version); err != nil {
		t.Fatalf("Approve() after attachment: %v", err)
	}
}

func TestMultiDiscAttachmentRejectsNonExactSetWithoutAdvancingDraft(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	baseUploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\nthree.chd\n")},
		{path: "game/one.chd", contents: fakeCHD("one")},
		{path: "game/two.chd", contents: fakeCHD("two")},
	})
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: baseUploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	testassert.False(t, err != nil, err)
	var itemID, baseSnapshotID string
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT item.id,draft.effective_source_snapshot_id
FROM import_items item JOIN review_drafts draft ON draft.import_item_id=item.id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&itemID, &baseSnapshotID); err != nil {
		t.Fatal(err)
	}
	attachmentUploadID := completeMultiDiscUpload(
		t, ctx, database, blobs, dataDir, "FILES", []multiDiscUploadFile{
			{path: "wrong.chd", contents: fakeCHD("wrong")},
		},
	)
	attachment, err := importer.CreateMultiDiscAttachment(ctx, itemID, 1, MultiDiscAttachmentRequest{
		UploadID: attachmentUploadID,
	})
	testassert.False(t, err != nil, err)
	waitParentJob(t, database.SQL, attachment.JobID, "FAILED")
	var state, errorCode, currentSnapshotID string
	var selectedID sql.NullString
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT attachment.state,attachment.error_code,draft.effective_source_snapshot_id,draft.selected_validation_id
FROM review_multidisc_attachments attachment
JOIN review_drafts draft ON draft.id=attachment.review_draft_id
WHERE attachment.id=?
`, attachment.AttachmentID).Scan(&state, &errorCode, &currentSnapshotID, &selectedID); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return state != "REJECTED" }, func() bool { return errorCode != MultiDiscAttachmentErrorSetMismatch }, func() bool { return currentSnapshotID != baseSnapshotID }, func() bool { return selectedID.Valid }), "rejected attachment = %s/%s snapshot=%s selected=%v", state, errorCode, currentSnapshotID, selectedID)
	var consumptions int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM upload_consumptions WHERE upload_session_id=?
`, attachmentUploadID).Scan(&consumptions); err != nil || consumptions != 0 {
		t.Fatalf("rejected upload consumptions = %d, error=%v", consumptions, err)
	}
	badUploadID := completeMultiDiscUpload(
		t, ctx, database, blobs, dataDir, "FILES", []multiDiscUploadFile{
			{path: "three.chd", contents: []byte("not-a-valid-chd")},
		},
	)
	bad, err := importer.CreateMultiDiscAttachment(ctx, itemID, 2, MultiDiscAttachmentRequest{UploadID: badUploadID})
	testassert.False(t, err != nil, err)
	waitParentJob(t, database.SQL, bad.JobID, "FAILED")
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT state,error_code FROM review_multidisc_attachments WHERE id=?
`, bad.AttachmentID).Scan(&state, &errorCode); err != nil ||
		state != "REJECTED" || errorCode != MultiDiscAttachmentErrorContentInvalid {
		t.Fatalf("bad CHD attachment = %s/%s, error=%v", state, errorCode, err)
	}
}

func TestMultiDiscAdmissionRejectsMissingPlaylistAndUnsupportedTargetWithoutConsumption(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	uploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "game/one.chd", contents: fakeCHD("one")},
		{path: "game/two.chd", contents: fakeCHD("two")},
	})
	request := CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	}
	if _, err := importer.Create(ctx, request); !errors.Is(err, ErrMultiDiscPlaylistMissing) {
		t.Fatalf("missing playlist error = %v", err)
	}
	request.TargetPlatformInstanceID = testsupport.MustPlatformInstanceID(t, database.SQL, "psx/pcsx_rearmed")
	if _, err := importer.Create(ctx, request); !errors.Is(err, ErrMultiDiscModeUnavailable) {
		t.Fatalf("unsupported target error = %v", err)
	}
	var imports, consumptions int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT (SELECT count(*) FROM import_jobs WHERE upload_session_id=?),
       (SELECT count(*) FROM upload_consumptions WHERE upload_session_id=?)
`, uploadID, uploadID).Scan(&imports, &consumptions); err != nil || imports != 0 || consumptions != 0 {
		t.Fatalf("rejected admission wrote imports=%d consumptions=%d error=%v", imports, consumptions, err)
	}
}
