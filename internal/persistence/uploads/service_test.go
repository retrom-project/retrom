package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	uploadservice "retrom/internal/service/uploads"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func TestUploadPartAndFinalization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "open database: %v", err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	testassert.Falsef(t, err != nil, "open blob store: %v", err)
	service := uploadservice.New(New(database.SQL), blobs, dataDir, time.Now)
	session, err := service.Create(
		ctx,
		uploadservice.CreateRequest{
			SourceType: "FILES",
			Files:      []uploadservice.FileDeclaration{{ClientFileID: "f1", RelativePath: "game.gba", SizeBytes: 5}},
		},
	)
	testassert.Falsef(t, err != nil, "create: %v", err)
	contents := []byte("retrom")[:5]
	digest := sha256.Sum256(contents)
	header := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := service.PutPart(ctx, session.ID, session.Files[0].ID, 0, "bytes 0-4/5", header, bytes.NewReader(contents)); err != nil {
		t.Fatalf("put part: %v", err)
	}
	if err := service.PutPart(ctx, session.ID, session.Files[0].ID, 0, "bytes 0-4/5", "sha-256=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:", bytes.NewReader(contents)); !errors.Is(
		err,
		uploadservice.ErrInvalid,
	) {
		t.Fatalf("mismatched replay error = %v", err)
	}
	current, err := service.Get(ctx, session.ID)
	testassert.Falsef(t, err != nil, "get: %v", err)
	jobID, _, err := service.Complete(ctx, session.ID, current.Version)
	testassert.Falsef(t, err != nil, "complete: %v", err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatalf("read job: %v", err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "finalization state = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	final, err := service.Get(ctx, session.ID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return final.State != "COMPLETE" }, func() bool { return final.Files[0].State != "COMPLETE" }), "final upload = %s/%s, error = %v", final.State, final.Files[0].State, err)
	var count int
	if err := database.SQL.QueryRowContext(ctx, "SELECT count(*) FROM blobs WHERE size_bytes=?", len(contents)).Scan(
		&count,
	); err != nil ||
		count != 1 {
		t.Fatalf("blob count = %d, error = %v", count, err)
	}
	var started, inputs int
	if err := database.SQL.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='STARTED'),
 (SELECT count(*) FROM job_input_snapshots WHERE job_id=? AND length(input_digest)=64)
 `, jobID, jobID).Scan(&started, &inputs); err != nil {
		t.Fatal(err)
	}
	if started != 1 || inputs != 1 {
		t.Fatalf("finalization lost claim event or immutable input: %d/%d", started, inputs)
	}
}

func TestCreateRejectsUnsafeAndDuplicatePaths(t *testing.T) {
	t.Parallel()
	for _, files := range [][]uploadservice.FileDeclaration{
		{{ClientFileID: "f", RelativePath: "../game.gba", SizeBytes: 1}},
		{{ClientFileID: "a", RelativePath: "game.gba", SizeBytes: 1}, {ClientFileID: "b", RelativePath: "game.gba", SizeBytes: 1}},
	} {
		t.Run(fmt.Sprint(len(files)), func(t *testing.T) {
			dataDir := t.TempDir()
			database, err := store.Open(context.Background(), filepath.Join(dataDir, "retrom.db"), time.Now)
			testassert.False(t, err != nil, err)
			defer func() { cleanup.Error("close", database.Close()) }()
			blobs, _ := blobstore.Open(dataDir)
			_, err = uploadservice.New(New(database.SQL),
				blobs,
				dataDir,
				time.Now,
			).Create(context.Background(), uploadservice.CreateRequest{SourceType: "FILES", Files: files})
			testassert.Truef(t, errors.Is(err, uploadservice.ErrInvalid), "create error = %v", err)
		})
	}
}

func TestCreateEnforcesProjectUploadPurposeShape(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	database, err := store.Open(context.Background(), filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	service := uploadservice.New(New(database.SQL), blobs, dataDir, time.Now)

	valid, err := service.Create(context.Background(), uploadservice.CreateRequest{
		Purpose: "PROJECT", SourceType: "FILES",
		Files: []uploadservice.FileDeclaration{{ClientFileID: "project", RelativePath: "game.ZIP", SizeBytes: 1}},
	})
	testassert.Falsef(t, err != nil, "create RPG archive: %v", err)
	if valid.Purpose != "PROJECT" {
		t.Fatalf("created purpose = %q", valid.Purpose)
	}
	loaded, err := service.Get(context.Background(), valid.ID)
	testassert.Falsef(t, err != nil, "get RPG upload: %v", err)
	if loaded.Purpose != "PROJECT" {
		t.Fatalf("loaded purpose = %q", loaded.Purpose)
	}
	ons, err := service.Create(context.Background(), uploadservice.CreateRequest{
		Purpose: "PROJECT", SourceType: "DIRECTORY",
		Files: []uploadservice.FileDeclaration{
			{ClientFileID: "script", RelativePath: "game/0.txt", SizeBytes: 1},
			{ClientFileID: "font", RelativePath: "game/default.ttf", SizeBytes: 1},
		},
	})
	testassert.Falsef(t, err != nil, "create ONS directory: %v", err)
	if ons.Purpose != "PROJECT" {
		t.Fatalf("ONS purpose = %q", ons.Purpose)
	}
	tyranoScript, err := service.Create(context.Background(), uploadservice.CreateRequest{
		Purpose: "PROJECT", SourceType: "FILES",
		Files: []uploadservice.FileDeclaration{{ClientFileID: "project", RelativePath: "game.EXE", SizeBytes: 1}},
	})
	testassert.Falsef(t, err != nil, "create TyranoScript NW.js executable: %v", err)
	if tyranoScript.Purpose != "PROJECT" {
		t.Fatalf("TyranoScript purpose = %q", tyranoScript.Purpose)
	}

	invalidRequests := []uploadservice.CreateRequest{
		{Purpose: "UNKNOWN", SourceType: "DIRECTORY", Files: []uploadservice.FileDeclaration{
			{ClientFileID: "project", RelativePath: "game/Game.ini", SizeBytes: 1},
		}},
		{Purpose: "RPG_MAKER_PROJECT", SourceType: "FILES", Files: []uploadservice.FileDeclaration{{ClientFileID: "project", RelativePath: "game.exe", SizeBytes: 1}}},
		{Purpose: "PROJECT", SourceType: "FILES", Files: []uploadservice.FileDeclaration{{ClientFileID: "project", RelativePath: "game.dat", SizeBytes: 1}}},
		{Purpose: "RUNTIME_ASSET_PACK", SourceType: "FILES", Files: []uploadservice.FileDeclaration{
			{ClientFileID: "a", RelativePath: "a.zip", SizeBytes: 1},
			{ClientFileID: "b", RelativePath: "b.zip", SizeBytes: 1},
		}},
	}
	for _, request := range invalidRequests {
		if _, err := service.Create(context.Background(), request); !errors.Is(err, uploadservice.ErrInvalid) {
			t.Fatalf("Create(%#v) error = %v", request, err)
		}
	}
}

func TestCancelCreatedUploadIsVersionedAndTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	service := uploadservice.New(New(database.SQL), blobs, dataDir, time.Now)
	session, err := service.Create(
		ctx,
		uploadservice.CreateRequest{
			SourceType: "FILES",
			Files:      []uploadservice.FileDeclaration{{ClientFileID: "f1", RelativePath: "game.gba", SizeBytes: 1}},
		},
	)
	testassert.False(t, err != nil, err)
	canceled, pending, err := service.Cancel(ctx, session.ID, session.Version)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending }, func() bool { return canceled.State != "CANCELLED" }, func() bool { return canceled.Version != session.Version+1 }), "cancel = %#v, pending=%v, error=%v", canceled, pending, err)
	if _, _, err := service.Cancel(ctx, session.ID, canceled.Version); !errors.Is(err, uploadservice.ErrInvalid) {
		t.Fatalf("terminal cancel error = %v", err)
	}
	if _, _, err := service.Complete(ctx, session.ID, canceled.Version); !errors.Is(err, uploadservice.ErrInvalid) {
		t.Fatalf("complete canceled upload error = %v", err)
	}
}
