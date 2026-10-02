package libraryimport

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"retrom/internal/mediaasset"
)

func TestReviewVideoUploadValidation(t *testing.T) {
	for _, test := range []struct {
		name      string
		contents  []byte
		size      int64
		wantType  string
		wantError error
	}{
		{"mp4", []byte{0, 0, 0, 12, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 12, "video/mp4", nil},
		{"webm", []byte{0x1a, 0x45, 0xdf, 0xa3, 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}, 11, "video/webm", nil},
		{"disguised image", []byte("not a video"), 11, "", ErrReviewAssetVideoInvalid},
		{"empty", nil, 0, "", ErrReviewAssetVideoInvalid},
		{"oversize", nil, mediaasset.MaxVideoBytes + 1, "", ErrReviewAssetVideoInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, blobs := coverFixture(t)
			repository.source.SizeBytes = test.size
			repository.current = repository.source
			blobs.reader = io.NopCloser(bytes.NewReader(test.contents))
			request := coverRequest()
			request.Kind = "VIDEO"
			result, err := coverService(repository, blobs).Upload(t.Context(), request)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("upload error = %v, want %v", err, test.wantError)
			}
			if test.wantError != nil {
				if repository.transactions != 0 {
					t.Fatal("invalid video entered write transaction")
				}
				return
			}
			if result.Kind != "VIDEO" || result.MediaType != test.wantType || result.Width != nil || result.Height != nil || repository.consumptions != 1 {
				t.Fatalf("video result = %+v, consumption count = %d", result, repository.consumptions)
			}
		})
	}
}

func TestReviewApprovalManualVideoOverridesSource(t *testing.T) {
	video := "manual-video"
	run := reviewApprovalRun{head: ReviewApprovalHead{UploadedVideoID: &video}, origin: ApprovalOrigin{
		Assets: []ApprovalExternalAsset{{Kind: "VIDEO", FileRecord: "unavailable-source-video", MediaType: "invalid"}},
	}}
	if err := run.appendExternalAssets(); err != nil || len(run.assets) != 0 {
		t.Fatalf("unused source video blocked manual choice: assets=%v err=%v", run.assets, err)
	}
	run.head.UploadedVideoID = nil
	if err := run.appendExternalAssets(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("restoring source bypassed validation: %v", err)
	}
}
