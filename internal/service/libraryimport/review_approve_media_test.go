package libraryimport

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/libraryimport"
)

type approvalMediaStub struct {
	model.ApprovalMediaReader
	found bool
	cause error
	asset model.ApprovalExternalAsset
	reads int
}

func (stub *approvalMediaStub) Candidate(context.Context, string, string) (model.ApprovalExternalAsset, bool, error) {
	stub.reads++
	return stub.asset, stub.found, stub.cause
}

func (stub *approvalMediaStub) UploadedCover(context.Context, string, string) (model.ApprovalExternalAsset, bool, error) {
	stub.reads++
	return stub.asset, stub.found, stub.cause
}

func (stub *approvalMediaStub) BlobExists(context.Context, string) (bool, error) {
	stub.reads++
	return stub.found, stub.cause
}

func TestReviewApprovalManualCoverOverridesUnusedSourceCover(t *testing.T) {
	cover := "manual-cover"
	run := reviewApprovalRun{head: model.ReviewApprovalHead{UploadedCoverID: &cover}, origin: model.ApprovalOrigin{
		Assets: []model.ApprovalExternalAsset{{Kind: "COVER", BlobID: "unavailable-source-cover", MediaType: "invalid"}},
	}}
	if err := run.appendExternalAssets(); err != nil || len(run.assets) != 0 {
		t.Fatalf("unused cover blocked manual choice: assets=%v err=%v", run.assets, err)
	}
}

func TestReviewApprovalExternalMediaRequiresExistingBlob(t *testing.T) {
	cause := errors.New("source asset read failed")
	for _, test := range []struct {
		name             string
		found            bool
		cause, errorWant error
	}{
		{"missing", false, nil, model.ErrInvalid}, {"unavailable", false, cause, cause}, {"valid", true, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			media := &approvalMediaStub{found: test.found, cause: test.cause}
			run := reviewApprovalRun{ctx: t.Context(), scope: model.ReviewApprovalScope{Media: media}, origin: model.ApprovalOrigin{
				Assets: []model.ApprovalExternalAsset{{Kind: "VIDEO", BlobID: "video", MediaType: "video/mp4"}},
			}}
			err := run.appendExternalAssets()
			if !errors.Is(err, test.errorWant) || media.reads != 1 {
				t.Fatalf("reads=%d err=%v", media.reads, err)
			}
			if (len(run.assets) == 1) != (err == nil) {
				t.Fatalf("assets=%v err=%v", run.assets, err)
			}
		})
	}
}

func TestReviewApprovalSelectedMediaPreservesReadCause(t *testing.T) {
	cause := errors.New("selected media read failed")
	for _, uploaded := range []bool{false, true} {
		media := &approvalMediaStub{cause: cause}
		run := reviewApprovalRun{ctx: t.Context(), scope: model.ReviewApprovalScope{Media: media}}
		if err := run.appendSelectedAsset("selection", "COVER", 0, uploaded); !errors.Is(err, cause) || len(run.assets) != 0 {
			t.Fatalf("uploaded=%v assets=%v err=%v", uploaded, run.assets, err)
		}
	}
}
