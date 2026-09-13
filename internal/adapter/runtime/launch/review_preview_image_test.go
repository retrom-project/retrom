package launch

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"retrom/internal/adapter/files/blobstore"
)

func TestInspectReviewScreenshotAcceptsRuntimeJPEG(t *testing.T) {
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 220, G: 70, B: 40, A: 255})
	var payload bytes.Buffer
	if err := jpeg.Encode(&payload, canvas, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}

	result, err := (screenshotImages{blobs: blobs}).Read(t.Context(), bytes.NewReader(payload.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if result.MediaType != "image/jpeg" || result.WidthPX != 2 || result.HeightPX != 2 {
		t.Fatalf("review JPEG=%#v", result)
	}
}
