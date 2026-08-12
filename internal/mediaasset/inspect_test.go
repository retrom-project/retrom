package mediaasset

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestInspectImageAndVideoContainers(t *testing.T) {
	t.Parallel()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	image, err := InspectImage(bytes.NewReader(png), int64(len(png)))
	if err != nil || image.MediaType != "image/png" || image.WidthPX != 1 || image.HeightPX != 1 {
		t.Fatalf("image = %#v, error=%v", image, err)
	}
	mp4 := []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm'}
	if mediaType, err := InspectVideo(bytes.NewReader(mp4), int64(len(mp4))); err != nil || mediaType != "video/mp4" {
		t.Fatalf("mp4 = %q, error=%v", mediaType, err)
	}
	webm := []byte{0x1a, 0x45, 0xdf, 0xa3, 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}
	if mediaType, err := InspectVideo(bytes.NewReader(webm), int64(len(webm))); err != nil || mediaType != "video/webm" {
		t.Fatalf("webm = %q, error=%v", mediaType, err)
	}
}

func TestInspectRejectsSpoofedAndOversizedMedia(t *testing.T) {
	t.Parallel()
	if _, err := InspectImage(bytes.NewReader([]byte("not an image")), 12); !errors.Is(err, ErrImageInvalid) {
		t.Fatalf("image error = %v", err)
	}
	if _, err := InspectVideo(bytes.NewReader([]byte("not a video")), 11); !errors.Is(err, ErrVideoUnsupported) {
		t.Fatalf("video error = %v", err)
	}
	if _, err := InspectVideo(bytes.NewReader(nil), MaxVideoBytes+1); !errors.Is(err, ErrVideoTooLarge) {
		t.Fatalf("large video error = %v", err)
	}
}
