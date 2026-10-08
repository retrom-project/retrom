package saves

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func TestScreenshotAcceptsActualPNGAndJPEGBytes(t *testing.T) {
	t.Parallel()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	s := &Service{Storage: store}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			if format == "png" {
				err = png.Encode(&encoded, picture)
			} else {
				err = jpeg.Encode(&encoded, picture, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			key, writeErr := s.screenshot(t.Context(), uuid.NewString(), bytes.NewReader(encoded.Bytes()))
			if writeErr != nil {
				t.Fatal(writeErr)
			}
			stored, readErr := store.Read(key)
			if readErr != nil {
				t.Fatal(readErr)
			}
			raw, readErr := io.ReadAll(stored)
			closeErr := stored.Close()
			if readErr != nil || closeErr != nil || !bytes.Equal(raw, encoded.Bytes()) {
				t.Fatalf("screenshot bytes changed: read=%v close=%v", readErr, closeErr)
			}
			_, detected, decodeErr := image.Decode(bytes.NewReader(raw))
			if decodeErr != nil || detected != format {
				t.Fatalf("format=%q decode=%v", detected, decodeErr)
			}
		})
	}
}

func TestScreenshotRejectsUnrecognizedOrInvalidImages(t *testing.T) {
	t.Parallel()
	s := &Service{}
	for _, raw := range [][]byte{nil, []byte("not an image"), []byte("GIF89a\x02\x00\x02\x00"), {0xff, 0xd8, 0xff}} {
		if _, err := s.screenshot(t.Context(), uuid.NewString(), bytes.NewReader(raw)); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid screenshot accepted: %v", err)
		}
	}
}
