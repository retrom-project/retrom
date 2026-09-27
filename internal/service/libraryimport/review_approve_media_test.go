package libraryimport

import (
	"context"
	"errors"
	"testing"
)

type approvalMediaStub struct {
	ApprovalMediaReader
	found bool
	cause error
	asset ApprovalExternalAsset
	reads int
}

func (stub *approvalMediaStub) Candidate(context.Context, string, string) (ApprovalExternalAsset, bool, error) {
	stub.reads++
	return stub.asset, stub.found, stub.cause
}

func (stub *approvalMediaStub) UploadedCover(context.Context, string, string) (ApprovalExternalAsset, bool, error) {
	stub.reads++
	return stub.asset, stub.found, stub.cause
}

func TestReviewApprovalManualCoverOverridesUnusedSourceCover(t *testing.T) {
	cover := "manual-cover"
	run := reviewApprovalRun{head: ReviewApprovalHead{UploadedCoverID: &cover}, origin: ApprovalOrigin{
		Assets: []ApprovalExternalAsset{{Kind: "COVER", FileRecord: "unavailable-source-cover", MediaType: "invalid"}},
	}}
	if err := run.appendExternalAssets(); err != nil || len(run.assets) != 0 {
		t.Fatalf("unused cover blocked manual choice: assets=%v err=%v", run.assets, err)
	}
}

func TestReviewApprovalSelectedMediaPreservesReadCause(t *testing.T) {
	cause := errors.New("selected media read failed")
	for _, uploaded := range []bool{false, true} {
		media := &approvalMediaStub{cause: cause}
		run := reviewApprovalRun{ctx: t.Context(), scope: ReviewApprovalScope{Media: media}}
		if err := run.appendSelectedAsset("selection", "COVER", 0, uploaded); !errors.Is(err, cause) || len(run.assets) != 0 {
			t.Fatalf("uploaded=%v assets=%v err=%v", uploaded, run.assets, err)
		}
	}
}
