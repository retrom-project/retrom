package detector

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	policy "retrom/internal/capability/engine/rpgmaker/detector"
	model "retrom/internal/model/libraryimport"
)

func TestPreparedDetectorBorrowsUnpublishedCandidatesUntilCallerDiscards(t *testing.T) {
	t.Parallel()
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := namedInput(t, "io/read")
	files := make([]model.RPGMakerProbeFile, 0, len(input.Contents))
	candidates := make([]*blobstore.Candidate, 0, len(input.Contents))
	for _, file := range memoryFixture(input.Contents).Files() {
		candidate, err := store.Stage(bytes.NewReader(input.Contents[file.Path]))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := candidate.Discard(); err != nil {
				t.Error(err)
			}
		})
		candidates = append(candidates, candidate)
		files = append(files, model.RPGMakerProbeFile{File: file, SourcePath: candidate.Path()})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	profile, err := (PreparedDetector{}).DetectPrepared(ctx, policy.VirtualCoreID, files)
	if err != nil || profile.ExpectedGeneration != policy.RPG2003 || profile.EvidenceGeneration == nil {
		t.Fatalf("prepared candidate detection = %#v, %v", profile, err)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate.Path()); err != nil {
			t.Fatalf("detector consumed candidate: %v", err)
		}
		if _, err := os.Stat(store.Path(candidate.Metadata().SHA256)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("detector published candidate: %v", err)
		}
		name := candidate.Path()
		if err := candidate.Discard(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("caller discard failed: %v", err)
		}
	}
}

func TestPreparedDetectorPreservesPathOpenCauseAndDoesNotOpenUnneededFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	input := namedInput(t, "io/read")
	files := make([]model.RPGMakerProbeFile, 0, len(input.Contents))
	for _, file := range memoryFixture(input.Contents).Files() {
		files = append(files, model.RPGMakerProbeFile{File: file, SourcePath: filepath.Join(root, file.Path)})
	}
	_, err := (PreparedDetector{}).DetectPrepared(t.Context(), "rpgmaker_2003", files)
	var typed *policy.Error
	var pathError *os.PathError
	if !errors.As(err, &typed) || !errors.As(err, &pathError) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing prepared input lost error identity: %v", err)
	}
	if typed.Code != policy.CodeLCFInvalid || typed.ExpectedGeneration != policy.RPG2003 ||
		!strings.HasPrefix(errors.Unwrap(err).Error(), "open RPG Maker project file: ") {
		t.Fatalf("prepared open error = %#v, %v", typed, err)
	}
	for _, file := range files {
		if file.File.Path == "Map0001.lmu" {
			continue
		}
		if err := os.WriteFile(file.SourcePath, input.Contents[file.File.Path], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := (PreparedDetector{}).DetectPrepared(t.Context(), policy.VirtualCoreID, files)
	if err != nil || profile.ExpectedGeneration != policy.RPG2003 {
		t.Fatalf("unread start-map bytes became required: %#v, %v", profile, err)
	}
}
