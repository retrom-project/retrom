package detector

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	policy "retrom/internal/capability/engine/rpgmaker/detector"
	model "retrom/internal/model/gamecontent"
)

func storedRPGFixture(t *testing.T) (*blobstore.Store, []model.RPGMakerBlobFile) {
	t.Helper()
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := namedInput(t, "io/read")
	files := make([]model.RPGMakerBlobFile, 0, len(input.Contents))
	for _, file := range memoryFixture(input.Contents).Files() {
		digest := "unread-start-map"
		if file.Path != "Map0001.lmu" {
			metadata, err := store.Put(bytes.NewReader(input.Contents[file.Path]))
			if err != nil {
				t.Fatal(err)
			}
			digest = metadata.SHA256
		}
		files = append(files, model.RPGMakerBlobFile{File: file, SHA256: digest})
	}
	return store, files
}

func TestBlobDetectorRetainsOldContextAndSelectiveDigestReads(t *testing.T) {
	t.Parallel()
	store, files := storedRPGFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	profile, err := NewBlobDetector(store).DetectBlobs(ctx, policy.VirtualCoreID, files)
	if err != nil || profile.ExpectedGeneration != policy.RPG2003 {
		t.Fatalf("blob detection changed cancellation/selective reads: %#v, %v", profile, err)
	}
	for position := range files {
		if files[position].File.Path == "RPG_RT.ldb" {
			files[position].SHA256 = "missing"
		}
	}
	_, err = NewBlobDetector(store).DetectBlobs(t.Context(), "rpgmaker_2003", files)
	var typed *policy.Error
	if !errors.As(err, &typed) || !errors.Is(err, os.ErrNotExist) ||
		typed.Code != policy.CodeLCFInvalid || typed.ExpectedGeneration != policy.RPG2003 {
		t.Fatalf("blob open cause = %#v, %v", typed, err)
	}
	if !strings.HasPrefix(errors.Unwrap(err).Error(), "open RPG replacement file: ") {
		t.Fatalf("blob open wrapper changed: %v", errors.Unwrap(err))
	}
}

func TestBlobDetectorRequiresAStore(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("missing store was accepted")
		}
	}()
	NewBlobDetector(nil)
}
