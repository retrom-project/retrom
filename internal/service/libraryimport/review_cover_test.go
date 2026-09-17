package libraryimport

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/libraryimport"
)

type coverRepositoryFixture struct {
	source, current                      model.ReviewCoverSource
	draft                                model.ReviewCoverDraft
	existing                             model.ReviewCoverExisting
	sourceError, commitError, draftError error
	assetWrites, consumptions            int
	committed                            bool
}

func (fixture *coverRepositoryFixture) Source(_ context.Context, _ string) (model.ReviewCoverSource, bool, error) {
	source := fixture.source
	return source, source.FileID != "", fixture.sourceError
}

func (fixture *coverRepositoryFixture) CommitCoverUpload(_ context.Context, cmd model.ReviewCoverUploadCommand) (model.ReviewCoverRecord, error) {
	fixture.committed = true

	currentSource := fixture.current
	if currentSource.FileID == "" || currentSource != cmd.Source {
		return model.ReviewCoverRecord{}, model.ErrReviewCoverUploadInvalid
	}
	draft := fixture.draft
	if draft.Version == 0 {
		return model.ReviewCoverRecord{}, model.ErrReviewCoverVersion
	}
	if fixture.draftError != nil {
		return model.ReviewCoverRecord{}, fixture.draftError
	}
	if draft.Version != cmd.Request.ExpectedVersion || draft.State != "REVIEW_PENDING" ||
		(draft.HandoffKind != "DIRECT" && !draft.EmulationStationReady) || draft.SourceBusy {
		return model.ReviewCoverRecord{}, model.ErrReviewCoverVersion
	}

	existing := fixture.existing
	if existing.Record.ID != "" {
		if existing.Record.ItemID != cmd.Request.ItemID {
			return model.ReviewCoverRecord{}, model.ErrReviewCoverConsumed
		}
		if !existing.HasConsumption || existing.Record.BlobID != cmd.Source.BlobID {
			return model.ReviewCoverRecord{}, model.ErrReviewCoverIntegrity
		}
		return existing.Record, nil
	}

	if fixture.commitError != nil {
		return model.ReviewCoverRecord{}, fixture.commitError
	}

	fixture.assetWrites++
	fixture.consumptions++
	return model.ReviewCoverRecord{
		ID: cmd.AssetID, ItemID: cmd.Request.ItemID, UploadFileID: cmd.Source.FileID,
		BlobID: cmd.Source.BlobID, Width: cmd.Width, Height: cmd.Height,
		MediaType: cmd.MediaType, CreatedAtMS: cmd.NowMS,
	}, nil
}

type coverBlobFixture struct {
	reader     io.ReadCloser
	beforeOpen func()
	openError  error
}

func (blobs coverBlobFixture) OpenDigest(string) (io.ReadCloser, error) {
	if blobs.beforeOpen != nil {
		blobs.beforeOpen()
	}
	return blobs.reader, blobs.openError
}

type coverReadCloser struct {
	io.Reader
	closeError error
}

func (reader coverReadCloser) Close() error { return reader.closeError }

type coverFailedReader struct{ err error }

func (reader coverFailedReader) Read([]byte) (int, error) { return 0, reader.err }

func coverRequest() model.ReviewCoverRequest {
	return model.ReviewCoverRequest{ItemID: "item", UploadFileID: "upload", Kind: "COVER", ExpectedVersion: 1}
}

func coverFixture(t *testing.T) (*coverRepositoryFixture, coverBlobFixture) {
	t.Helper()
	source := model.ReviewCoverSource{FileID: "upload", UploadID: "session", BlobID: "blob", Digest: "digest", Purpose: "GENERAL"}
	repository := &coverRepositoryFixture{
		source: source, current: source,
		draft: model.ReviewCoverDraft{Version: 1, State: "REVIEW_PENDING", HandoffKind: "DIRECT"},
	}
	var contents bytes.Buffer
	if err := png.Encode(&contents, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return repository, coverBlobFixture{reader: io.NopCloser(bytes.NewReader(contents.Bytes()))}
}

func coverService(repository model.ReviewCoverRepository, blobs model.ReviewCoverBlobs) *ReviewCoverUploads {
	return NewReviewCoverUploads(repository, blobs, func() time.Time { return time.UnixMilli(100) })
}

func TestReviewCoverPreservesSourceReadCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("source query unavailable")
	service := coverService(&coverRepositoryFixture{sourceError: cause}, nil)
	result, err := service.Upload(t.Context(), coverRequest())
	if !errors.Is(err, cause) || errors.Is(err, model.ErrReviewCoverUploadInvalid) || result != (model.ReviewCoverResult{}) {
		t.Fatalf("source failure changed classification or leaked result: result=%+v err=%v", result, err)
	}
}

func TestReviewCoverPreparationErrorsPreserveCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("CAS unavailable")
	for _, test := range []struct {
		name  string
		blobs coverBlobFixture
	}{
		{"open", coverBlobFixture{openError: cause}},
		{"read", coverBlobFixture{reader: coverReadCloser{Reader: coverFailedReader{cause}}}},
		{"close", coverBlobFixture{reader: coverReadCloser{Reader: strings.NewReader(""), closeError: cause}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, _ := coverFixture(t)
			result, err := coverService(repository, test.blobs).Upload(t.Context(), coverRequest())
			if !errors.Is(err, cause) || !errors.Is(err, model.ErrReviewCoverCASUnavailable) || result != (model.ReviewCoverResult{}) || repository.committed {
				t.Fatalf("CAS failure lost cause or entered transaction: result=%+v err=%v committed=%v", result, err, repository.committed)
			}
		})
	}
}

func TestReviewCoverInvalidSourcesAndImagesDoNotWrite(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		mutate   func(*coverRepositoryFixture, *coverBlobFixture)
		expected error
	}{
		{"missing", func(repo *coverRepositoryFixture, _ *coverBlobFixture) { repo.source = model.ReviewCoverSource{} }, model.ErrReviewCoverUploadInvalid},
		{"project upload", func(repo *coverRepositoryFixture, _ *coverBlobFixture) { repo.source.Purpose = "PROJECT" }, model.ErrReviewCoverConsumed},
		{"invalid image", func(_ *coverRepositoryFixture, blobs *coverBlobFixture) {
			blobs.reader = io.NopCloser(strings.NewReader("not an image"))
		}, model.ErrReviewCoverImageInvalid},
		{"oversized image", func(_ *coverRepositoryFixture, blobs *coverBlobFixture) {
			blobs.reader = io.NopCloser(strings.NewReader(strings.Repeat("x", (10<<20)+1)))
		}, model.ErrReviewCoverImageInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			test.mutate(repository, &blobs)
			result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
			if !errors.Is(err, test.expected) || result != (model.ReviewCoverResult{}) || repository.committed {
				t.Fatalf("invalid preparation reached transaction: result=%+v err=%v committed=%v", result, err, repository.committed)
			}
		})
	}
}

func TestReviewCoverRechecksAuthorityAfterPreparation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		mutate   func(*coverRepositoryFixture)
		expected error
	}{
		{"changed blob", func(repo *coverRepositoryFixture) { repo.current.BlobID = "new-blob" }, model.ErrReviewCoverUploadInvalid},
		{"lost upload", func(repo *coverRepositoryFixture) { repo.current = model.ReviewCoverSource{} }, model.ErrReviewCoverUploadInvalid},
		{"stale draft", func(repo *coverRepositoryFixture) { repo.draft.Version++ }, model.ErrReviewCoverVersion},
		{"terminal item", func(repo *coverRepositoryFixture) { repo.draft.State = "DISCARDED" }, model.ErrReviewCoverVersion},
		{"reserved source", func(repo *coverRepositoryFixture) { repo.draft.HandoffKind = "EMULATIONSTATION" }, model.ErrReviewCoverVersion},
		{"busy owner", func(repo *coverRepositoryFixture) { repo.draft.SourceBusy = true }, model.ErrReviewCoverVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			test.mutate(repository)
			result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
			if !errors.Is(err, test.expected) || result != (model.ReviewCoverResult{}) || repository.assetWrites != 0 || repository.consumptions != 0 {
				t.Fatalf("stale preparation accepted: result=%+v err=%v writes=%d/%d", result, err, repository.assetWrites, repository.consumptions)
			}
		})
	}
}

func TestReviewCoverOwnershipReplay(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, owner string
		retained    bool
		expected    error
	}{
		{"same owner", "item", true, nil},
		{"different owner", "other", true, model.ErrReviewCoverConsumed},
		{"lost consumption", "item", false, model.ErrReviewCoverIntegrity},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			repository.existing = model.ReviewCoverExisting{HasConsumption: test.retained, Record: model.ReviewCoverRecord{
				ID: "original", ItemID: test.owner, BlobID: "blob", Width: 2, Height: 3, MediaType: "image/png", CreatedAtMS: 42,
			}}
			result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
			if !errors.Is(err, test.expected) || repository.assetWrites != 0 || repository.consumptions != 0 {
				t.Fatalf("invalid replay: result=%+v err=%v writes=%d/%d", result, err, repository.assetWrites, repository.consumptions)
			}
			if test.expected == nil && (result.AssetID != "original" || result.CreatedAtMS != 42 || result.Version != 1) {
				t.Fatalf("replay changed immutable identity: %+v", result)
			}
		})
	}
}

func TestReviewCoverIDAndCommitErrorsDoNotLeakSuccess(t *testing.T) {
	t.Parallel()
	cause := errors.New("cover persistence unavailable")
	for _, test := range []struct {
		name     string
		failedID int
		commit   bool
	}{
		{"asset ID", 1, false}, {"consumption ID", 2, false}, {"commit", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			service := coverService(repository, blobs)
			calls := 0
			service.newID = func() (string, error) {
				calls++
				if calls == test.failedID {
					return "", cause
				}
				return "id", nil
			}
			if test.commit {
				repository.commitError = cause
			}
			result, err := service.Upload(t.Context(), coverRequest())
			if !errors.Is(err, cause) || result != (model.ReviewCoverResult{}) {
				t.Fatalf("failed commit leaked result: %+v err=%v", result, err)
			}
		})
	}
}

func TestReviewCoverReadyReservationAndDraftErrors(t *testing.T) {
	t.Parallel()
	t.Run("ready reservation", func(t *testing.T) {
		t.Parallel()
		repository, blobs := coverFixture(t)
		repository.draft.HandoffKind = "EMULATIONSTATION"
		repository.draft.EmulationStationReady = true
		result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
		if err != nil || result.Width != 2 || result.Height != 3 || result.CreatedAtMS != 100 || repository.assetWrites != 1 || repository.consumptions != 1 {
			t.Fatalf("ready review not accepted: %+v err=%v", result, err)
		}
	})
	t.Run("query failure", func(t *testing.T) {
		t.Parallel()
		repository, blobs := coverFixture(t)
		cause := errors.New("draft SQL failure")
		repository.draftError = cause
		result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
		if !errors.Is(err, cause) || errors.Is(err, model.ErrReviewCoverVersion) || result != (model.ReviewCoverResult{}) {
			t.Fatalf("draft query error mapped to conflict: %+v err=%v", result, err)
		}
	})
}
