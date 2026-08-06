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

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/store"
)

func TestUploadPartAndFinalization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("open blob store: %v", err)
	}
	service := New(database.SQL, blobs, dataDir, time.Now)
	session, err := service.Create(
		ctx,
		CreateRequest{
			SourceType: "FILES",
			Files:      []FileDeclaration{{ClientFileID: "f1", RelativePath: "game.gba", SizeBytes: 5}},
		},
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	contents := []byte("retrom")[:5]
	digest := sha256.Sum256(contents)
	header := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := service.PutPart(ctx, session.ID, session.Files[0].ID, 0, "bytes 0-4/5", header, bytes.NewReader(contents)); err != nil {
		t.Fatalf("put part: %v", err)
	}
	if err := service.PutPart(ctx, session.ID, session.Files[0].ID, 0, "bytes 0-4/5", "sha-256=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:", bytes.NewReader(contents)); !errors.Is(
		err,
		ErrInvalid,
	) {
		t.Fatalf("mismatched replay error = %v", err)
	}
	current, err := service.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	jobID, _, err := service.Complete(ctx, session.ID, current.Version)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatalf("read job: %v", err)
		}
		if state == "SUCCEEDED" {
			break
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("finalization state = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	final, err := service.Get(ctx, session.ID)
	if err != nil || final.State != "COMPLETE" || final.Files[0].State != "COMPLETE" {
		t.Fatalf("final upload = %s/%s, error = %v", final.State, final.Files[0].State, err)
	}
	var count int
	if err := database.SQL.QueryRowContext(ctx, "SELECT count(*) FROM blobs WHERE size_bytes=?", len(contents)).Scan(
		&count,
	); err != nil ||
		count != 1 {
		t.Fatalf("blob count = %d, error = %v", count, err)
	}
}

func TestCreateRejectsUnsafeAndDuplicatePaths(t *testing.T) {
	t.Parallel()
	for _, files := range [][]FileDeclaration{
		{{ClientFileID: "f", RelativePath: "../game.gba", SizeBytes: 1}},
		{{ClientFileID: "a", RelativePath: "game.gba", SizeBytes: 1}, {ClientFileID: "b", RelativePath: "game.gba", SizeBytes: 1}},
	} {
		t.Run(fmt.Sprint(len(files)), func(t *testing.T) {
			dataDir := t.TempDir()
			database, err := store.Open(context.Background(), filepath.Join(dataDir, "retrom.db"), time.Now)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { cleanup.Error("close", database.Close()) }()
			blobs, _ := blobstore.Open(dataDir)
			_, err = New(
				database.SQL,
				blobs,
				dataDir,
				time.Now,
			).Create(context.Background(), CreateRequest{SourceType: "FILES", Files: files})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("create error = %v", err)
			}
		})
	}
}

func TestCancelCreatedUploadIsVersionedAndTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, blobs, dataDir, time.Now)
	session, err := service.Create(
		ctx,
		CreateRequest{
			SourceType: "FILES",
			Files:      []FileDeclaration{{ClientFileID: "f1", RelativePath: "game.gba", SizeBytes: 1}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	canceled, pending, err := service.Cancel(ctx, session.ID, session.Version)
	if err != nil || pending || canceled.State != "CANCELLED" || canceled.Version != session.Version+1 {
		t.Fatalf("cancel = %#v, pending=%v, error=%v", canceled, pending, err)
	}
	if _, _, err := service.Cancel(ctx, session.ID, canceled.Version); !errors.Is(err, ErrInvalid) {
		t.Fatalf("terminal cancel error = %v", err)
	}
	if _, _, err := service.Complete(ctx, session.ID, canceled.Version); !errors.Is(err, ErrInvalid) {
		t.Fatalf("complete canceled upload error = %v", err)
	}
}
