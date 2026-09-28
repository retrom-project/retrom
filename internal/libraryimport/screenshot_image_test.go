package libraryimport

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	libraryservice "retrom/internal/service/libraryimport"

	"retrom/internal/filestore"
	"retrom/internal/mediaasset"
)

type screenshotUnlimitedBytes struct{ count int64 }

func (source *screenshotUnlimitedBytes) Read(output []byte) (int, error) {
	clear(output)
	source.count += int64(len(output))
	return len(output), nil
}

func TestScreenshotImagesBoundsStreamAndDiscardsInvalidCandidate(t *testing.T) {
	root := t.TempDir()
	blobs, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	source := &screenshotUnlimitedBytes{}
	result, err := (screenshotImages{blobs: blobs}).Read(t.Context(), "018fbe68-0000-7000-8000-000000000001", source)
	if !errors.Is(err, libraryservice.ErrReviewScreenshotInvalid) || result.SHA256 != "" || source.count != mediaasset.MaxImageBytes+1 {
		t.Fatalf("image=%+v error=%v bytes read=%d", result, err, source.count)
	}
	files, err := os.ReadDir(filepath.Join(root, "staging", "writes"))
	if err != nil || len(files) != 0 {
		t.Fatalf("invalid candidate retained staging files=%d error=%v", len(files), err)
	}
}

func TestScreenshotImagesRejectsEmptyAndMalformedMedia(t *testing.T) {
	blobs, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []io.Reader{nil, bytes.NewReader(nil), bytes.NewReader([]byte("not an image"))} {
		result, err := (screenshotImages{blobs: blobs}).Read(t.Context(), "018fbe68-0000-7000-8000-000000000001", source)
		if !errors.Is(err, libraryservice.ErrReviewScreenshotInvalid) || result.SHA256 != "" {
			t.Fatalf("invalid media result=%+v error=%v", result, err)
		}
	}
}

func TestScreenshotImagesPreservesStagingAndPublishingCauses(t *testing.T) {
	for _, location := range []string{"staging/writes", "staging/items"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			blobs, err := filestore.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, location)
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("unavailable directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			var contents bytes.Buffer
			if err := png.Encode(&contents, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			result, err := (screenshotImages{blobs: blobs}).Read(t.Context(), "018fbe68-0000-7000-8000-000000000001", &contents)
			var storage *os.PathError
			if (!errors.As(err, &storage) && !errors.Is(err, filestore.ErrRecordInvalid)) ||
				errors.Is(err, libraryservice.ErrReviewScreenshotInvalid) || result.SHA256 != "" {
				t.Fatalf("filesystem failure result=%+v error=%v", result, err)
			}
		})
	}
}

func TestInspectScreenshotFilePreservesOpenCause(t *testing.T) {
	_, err := inspectScreenshotFile(filestore.Metadata{Path: filepath.Join(t.TempDir(), "missing"), Size: 100})
	if !errors.Is(err, os.ErrNotExist) || errors.Is(err, libraryservice.ErrReviewScreenshotInvalid) {
		t.Fatal(err)
	}
}
