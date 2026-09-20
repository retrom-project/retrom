//go:build linux

package detector

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

func TestBlobDetectorRetainsOpenDigestNoFollow(t *testing.T) {
	t.Parallel()
	store, files := storedRPGFixture(t)
	var blobPath string
	for _, file := range files {
		if file.File.Path == "RPG_RT.ldb" {
			blobPath = store.Path(file.SHA256)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside-ldb")
	if err := os.Rename(blobPath, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, blobPath); err != nil {
		t.Fatal(err)
	}
	_, err := NewBlobDetector(store).DetectBlobs(t.Context(), policy.VirtualCoreID, files)
	var typed *policy.Error
	var pathError *os.PathError
	if !errors.As(err, &typed) || !errors.As(err, &pathError) || !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("blob symlink did not retain no-follow failure: %v", err)
	}
	if typed.Code != policy.CodeLCFInvalid || pathError.Path != blobPath {
		t.Fatalf("no-follow failure changed: %#v / %#v", typed, pathError)
	}
}
