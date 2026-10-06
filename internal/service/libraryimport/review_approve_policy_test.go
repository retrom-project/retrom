package libraryimport

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestReviewApprovalInputBoundaries(t *testing.T) {
	reason := "  批准\n发布  "
	valid := ReviewApprovalRequest{ItemID: "item", ExpectedVersion: 1, Decision: ReviewApprovalDecision{Reason: &reason}}
	normalized, err := normalizeReviewApproval(valid)
	if err != nil || *normalized.Decision.Reason != "批准\n发布" || reason != "  批准\n发布  " {
		t.Fatalf("normalized=%+v reason=%q err=%v", normalized, reason, err)
	}
	for _, value := range []string{strings.Repeat("界", 500), strings.Repeat("界", 501), "\t", "bad\x00reason"} {
		request := valid
		request.Decision.Reason = &value
		_, err := normalizeReviewApproval(request)
		wantValid := value == strings.Repeat("界", 500)
		if (err == nil) != wantValid || (err != nil && !errors.Is(err, ErrInvalid)) {
			t.Fatalf("reason runes=%d error=%v", len([]rune(value)), err)
		}
	}
	for _, version := range []int64{0, -1, math.MaxInt64} {
		request := valid
		request.ExpectedVersion = version
		if _, err := normalizeReviewApproval(request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("version=%d error=%v", version, err)
		}
	}
}

func TestReviewApprovalDecisionRequiresCompleteAuthority(t *testing.T) {
	tests := []struct {
		name     string
		decision ReviewApprovalDecision
	}{
		{"unknown origin", ReviewApprovalDecision{SourceKind: "USER"}},
		{"review origin override", ReviewApprovalDecision{SourceKind: "IMPORT_REVIEW"}},
		{"lowercase origin", ReviewApprovalDecision{SourceKind: "import_receive"}},
		{
			"media without origin",
			ReviewApprovalDecision{ExternalAssets: []ApprovalExternalAsset{{
				Kind:       "VIDEO",
				FileRecord: "blob", MediaType: "video/mp4",
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeReviewApproval(ReviewApprovalRequest{ItemID: "item", ExpectedVersion: 1, Decision: test.decision})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, kind := range []string{"IMPORT_RECEIVE"} {
		if !validApprovalDecision(ReviewApprovalDecision{SourceKind: kind}) {
			t.Fatal(kind)
		}
	}
}

func TestReviewApprovalExternalAssetBoundaries(t *testing.T) {
	positive, zero := int64(1), int64(0)
	cover := ApprovalExternalAsset{
		Kind: "COVER", FileRecord: "cover", MediaType: "image/webp",
		WidthPX: &positive, HeightPX: &positive,
	}
	video := ApprovalExternalAsset{Kind: "VIDEO", FileRecord: "video", MediaType: "video/webm"}
	if !ValidApprovalExternalAssets([]ApprovalExternalAsset{cover, video}) {
		t.Fatal("valid cover/video rejected")
	}
	tests := []struct {
		name   string
		assets []ApprovalExternalAsset
	}{
		{"duplicate kind", []ApprovalExternalAsset{cover, cover}},
		{"empty blob", []ApprovalExternalAsset{{Kind: "VIDEO", MediaType: "video/mp4"}}},
		{"unsupported type", []ApprovalExternalAsset{{
			Kind: "VIDEO", FileRecord: "blob",
			MediaType: "application/octet-stream",
		}}},
		{"video dimensions", []ApprovalExternalAsset{{
			Kind: "VIDEO", FileRecord: "blob",
			MediaType: "video/mp4", WidthPX: &positive,
		}}},
		{"empty dimensions", []ApprovalExternalAsset{{
			Kind: "COVER", FileRecord: "blob",
			MediaType: "image/png", WidthPX: &zero, HeightPX: &positive,
		}}},
		{"unknown kind", []ApprovalExternalAsset{{Kind: "SCREENSHOT", FileRecord: "blob", MediaType: "image/png"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if ValidApprovalExternalAssets(test.assets) {
				t.Fatal("invalid media accepted")
			}
		})
	}
}

func TestBulkPublicationRequiresEveryFrozenIdentity(t *testing.T) {
	complete := BulkPublicationIntent{
		BulkID: "bulk", JobID: "job", WorkerID: "worker",
		SourceSnapshotID: "source",
	}
	if !validBulkPublicationIntent(nil) || !validBulkPublicationIntent(&complete) {
		t.Fatal("valid identity rejected")
	}
	for index := range 4 {
		intent := complete
		fields := []*string{&intent.BulkID, &intent.JobID, &intent.WorkerID, &intent.SourceSnapshotID}
		*fields[index] = ""
		if validBulkPublicationIntent(&intent) {
			t.Fatalf("missing field %d accepted", index)
		}
	}
}
