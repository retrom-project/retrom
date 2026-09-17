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
)

type coverRepositoryFixture struct {
	source, current                         ReviewCoverSource
	draft                                   ReviewCoverDraft
	existing                                ReviewCoverExisting
	sourceError, draftError, commitError    error
	assetWrites, consumptions, transactions int
	insideTransaction                       bool
}

func (fixture *coverRepositoryFixture) Source(context.Context, string) (ReviewCoverSource, bool, error) {
	source := fixture.source
	if fixture.insideTransaction {
		source = fixture.current
	}
	return source, source.FileID != "", fixture.sourceError
}

func (fixture *coverRepositoryFixture) CommitWrite(_ context.Context, work func(ReviewCoverScope) error) error {
	fixture.transactions++
	fixture.insideTransaction = true
	defer func() { fixture.insideTransaction = false }()
	if err := work(ReviewCoverScope{Reader: fixture, Writer: fixture}); err != nil {
		return err
	}
	return fixture.commitError
}

func (fixture *coverRepositoryFixture) Draft(context.Context, string) (ReviewCoverDraft, bool, error) {
	return fixture.draft, fixture.draft.Version != 0, fixture.draftError
}

func (fixture *coverRepositoryFixture) ExistingByUpload(context.Context, string) (ReviewCoverExisting, bool, error) {
	return fixture.existing, fixture.existing.Record.ID != "", nil
}

func (fixture *coverRepositoryFixture) InsertAsset(context.Context, ReviewCoverRecord) error {
	fixture.assetWrites++
	return nil
}

func (fixture *coverRepositoryFixture) Consume(context.Context, ReviewCoverConsumption) error {
	fixture.consumptions++
	return nil
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

func coverRequest() ReviewCoverRequest {
	return ReviewCoverRequest{ItemID: "item", UploadFileID: "upload", Kind: "COVER", ExpectedVersion: 1}
}

func coverFixture(t *testing.T) (*coverRepositoryFixture, coverBlobFixture) {
	t.Helper()
	source := ReviewCoverSource{FileID: "upload", UploadID: "session", BlobID: "blob", Digest: "digest", Purpose: "GENERAL"}
	repository := &coverRepositoryFixture{
		source: source, current: source,
		draft: ReviewCoverDraft{Version: 1, State: "REVIEW_PENDING", HandoffKind: "DIRECT"},
	}
	var contents bytes.Buffer
	if err := png.Encode(&contents, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return repository, coverBlobFixture{reader: io.NopCloser(bytes.NewReader(contents.Bytes()))}
}

func coverService(repository ReviewCoverRepository, blobs ReviewCoverBlobs) *ReviewCoverUploads {
	return NewReviewCoverUploads(repository, blobs, func() time.Time { return time.UnixMilli(100) })
}

func TestReviewCoverPreservesSourceReadCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("source query unavailable")
	service := coverService(&coverRepositoryFixture{sourceError: cause}, nil)
	result, err := service.Upload(t.Context(), coverRequest())
	if !errors.Is(err, cause) || errors.Is(err, ErrReviewCoverUploadInvalid) || result != (ReviewCoverResult{}) {
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
			if !errors.Is(err, cause) || !errors.Is(err, ErrReviewCoverCASUnavailable) || result != (ReviewCoverResult{}) || repository.transactions != 0 {
				t.Fatalf("CAS failure lost cause or entered transaction: result=%+v err=%v transactions=%d", result, err, repository.transactions)
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
		{"missing", func(repo *coverRepositoryFixture, _ *coverBlobFixture) { repo.source = ReviewCoverSource{} }, ErrReviewCoverUploadInvalid},
		{"project upload", func(repo *coverRepositoryFixture, _ *coverBlobFixture) { repo.source.Purpose = "PROJECT" }, ErrReviewCoverConsumed},
		{"invalid image", func(_ *coverRepositoryFixture, blobs *coverBlobFixture) {
			blobs.reader = io.NopCloser(strings.NewReader("not an image"))
		}, ErrReviewCoverImageInvalid},
		{"oversized image", func(_ *coverRepositoryFixture, blobs *coverBlobFixture) {
			blobs.reader = io.NopCloser(strings.NewReader(strings.Repeat("x", (10<<20)+1)))
		}, ErrReviewCoverImageInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			test.mutate(repository, &blobs)
			result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
			if !errors.Is(err, test.expected) || result != (ReviewCoverResult{}) || repository.transactions != 0 {
				t.Fatalf("invalid preparation reached transaction: result=%+v err=%v transactions=%d", result, err, repository.transactions)
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
		{"changed blob", func(repo *coverRepositoryFixture) { repo.current.BlobID = "new-blob" }, ErrReviewCoverUploadInvalid},
		{"lost upload", func(repo *coverRepositoryFixture) { repo.current = ReviewCoverSource{} }, ErrReviewCoverUploadInvalid},
		{"stale draft", func(repo *coverRepositoryFixture) { repo.draft.Version++ }, ErrReviewCoverVersion},
		{"terminal item", func(repo *coverRepositoryFixture) { repo.draft.State = "DISCARDED" }, ErrReviewCoverVersion},
		{"reserved source", func(repo *coverRepositoryFixture) { repo.draft.HandoffKind = "EMULATIONSTATION" }, ErrReviewCoverVersion},
		{"busy owner", func(repo *coverRepositoryFixture) { repo.draft.SourceBusy = true }, ErrReviewCoverVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			blobs.beforeOpen = func() {
				if repository.insideTransaction {
					t.Fatal("CAS work held write transaction")
				}
				test.mutate(repository)
			}
			result, err := coverService(repository, blobs).Upload(t.Context(), coverRequest())
			if !errors.Is(err, test.expected) || result != (ReviewCoverResult{}) || repository.assetWrites != 0 || repository.consumptions != 0 {
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
		{"different owner", "other", true, ErrReviewCoverConsumed},
		{"lost consumption", "item", false, ErrReviewCoverIntegrity},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, blobs := coverFixture(t)
			repository.existing = ReviewCoverExisting{HasConsumption: test.retained, Record: ReviewCoverRecord{
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
			if !errors.Is(err, cause) || result != (ReviewCoverResult{}) {
				t.Fatalf("failed commit leaked result: %+v err=%v", result, err)
			}
			if !test.commit && (repository.assetWrites != 0 || repository.consumptions != 0) {
				t.Fatal("ID error occurred after writes")
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
		if !errors.Is(err, cause) || errors.Is(err, ErrReviewCoverVersion) || result != (ReviewCoverResult{}) {
			t.Fatalf("draft query error mapped to conflict: %+v err=%v", result, err)
		}
	})
}
