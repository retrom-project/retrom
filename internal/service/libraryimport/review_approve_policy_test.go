package libraryimport

import (
	"errors"
	"math"
	"strings"
	"testing"

	model "retrom/internal/model/libraryimport"
)

func TestReviewApprovalInputBoundaries(t *testing.T) {
	reason := "  批准\n发布  "
	valid := model.ReviewApprovalRequest{ItemID: "item", ExpectedVersion: 1, Decision: model.ReviewApprovalDecision{Reason: &reason}}
	normalized, err := normalizeReviewApproval(valid)
	if err != nil || *normalized.Decision.Reason != "批准\n发布" || reason != "  批准\n发布  " {
		t.Fatalf("normalized=%+v reason=%q err=%v", normalized, reason, err)
	}
	for _, value := range []string{strings.Repeat("界", 500), strings.Repeat("界", 501), "\t", "bad\x00reason"} {
		request := valid
		request.Decision.Reason = &value
		_, err := normalizeReviewApproval(request)
		wantValid := value == strings.Repeat("界", 500)
		if (err == nil) != wantValid || (err != nil && !errors.Is(err, model.ErrInvalid)) {
			t.Fatalf("reason runes=%d error=%v", len([]rune(value)), err)
		}
	}
	for _, version := range []int64{0, -1, math.MaxInt64} {
		request := valid
		request.ExpectedVersion = version
		if _, err := normalizeReviewApproval(request); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("version=%d error=%v", version, err)
		}
	}
}

func TestReviewApprovalDecisionRequiresCompleteAuthority(t *testing.T) {
	tests := []struct {
		name     string
		decision model.ReviewApprovalDecision
	}{
		{"unknown policy", model.ReviewApprovalDecision{DuplicatePolicy: "OVERWRITE"}},
		{"ack without policy", model.ReviewApprovalDecision{AcknowledgedGameIDs: []string{"game"}}},
		{"unknown origin", model.ReviewApprovalDecision{SourceKind: "USER", SourceRefID: "source"}},
		{"origin without ref", model.ReviewApprovalDecision{SourceKind: "SERVER_PEGASUS_IMPORT"}},
		{"ref without origin", model.ReviewApprovalDecision{SourceRefID: "source"}},
		{"media without origin", model.ReviewApprovalDecision{ExternalAssets: []model.ApprovalExternalAsset{{Kind: "VIDEO", BlobID: "blob", MediaType: "video/mp4"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeReviewApproval(model.ReviewApprovalRequest{ItemID: "item", ExpectedVersion: 1, Decision: test.decision})
			if !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, kind := range []string{"SERVER_PEGASUS_IMPORT", "SERVER_EMULATIONSTATION_IMPORT"} {
		if !validApprovalDecision(model.ReviewApprovalDecision{SourceKind: kind, SourceRefID: "source"}) {
			t.Fatal(kind)
		}
	}
}

func TestReviewApprovalExternalAssetBoundaries(t *testing.T) {
	positive, zero := int64(1), int64(0)
	cover := model.ApprovalExternalAsset{Kind: "COVER", BlobID: "cover", MediaType: "image/webp", WidthPX: &positive, HeightPX: &positive}
	video := model.ApprovalExternalAsset{Kind: "VIDEO", BlobID: "video", MediaType: "video/webm"}
	if !ValidApprovalExternalAssets([]model.ApprovalExternalAsset{cover, video}) {
		t.Fatal("valid cover/video rejected")
	}
	tests := []struct {
		name   string
		assets []model.ApprovalExternalAsset
	}{
		{"duplicate kind", []model.ApprovalExternalAsset{cover, cover}},
		{"empty blob", []model.ApprovalExternalAsset{{Kind: "VIDEO", MediaType: "video/mp4"}}},
		{"unsupported type", []model.ApprovalExternalAsset{{Kind: "VIDEO", BlobID: "blob", MediaType: "application/octet-stream"}}},
		{"video dimensions", []model.ApprovalExternalAsset{{Kind: "VIDEO", BlobID: "blob", MediaType: "video/mp4", WidthPX: &positive}}},
		{"empty dimensions", []model.ApprovalExternalAsset{{Kind: "COVER", BlobID: "blob", MediaType: "image/png", WidthPX: &zero, HeightPX: &positive}}},
		{"unknown kind", []model.ApprovalExternalAsset{{Kind: "SCREENSHOT", BlobID: "blob", MediaType: "image/png"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if ValidApprovalExternalAssets(test.assets) {
				t.Fatal("invalid media accepted")
			}
		})
	}
}

func TestReviewApprovalRequiresExactDuplicateAcknowledgment(t *testing.T) {
	games := []model.DuplicateGame{{GameID: "b"}, {GameID: "a"}}
	for _, test := range []struct {
		ids   []string
		valid bool
	}{
		{[]string{"a", "b"}, true},
		{[]string{"b", "a"}, true},
		{[]string{"a"}, false},
		{[]string{"a", "a"}, false},
		{[]string{"a", "c"}, false},
		{[]string{"a", "b", "c"}, false},
	} {
		if SameApprovalDuplicateIDs(games, test.ids) != test.valid {
			t.Fatalf("ack=%v", test.ids)
		}
	}
	if games[0].GameID != "b" {
		t.Fatal("duplicate projection mutated repository snapshot")
	}
	conflict := &model.DuplicateConflict{Games: games}
	if !errors.Is(conflict, model.ErrDuplicateContent) {
		t.Fatal("duplicate cause missing")
	}
}

func TestBulkPublicationRequiresEveryFrozenIdentity(t *testing.T) {
	complete := model.BulkPublicationIntent{BulkID: "bulk", JobID: "job", WorkerID: "worker", ValidationID: "validation", SourceSnapshotID: "source"}
	if !validBulkPublicationIntent(nil) || !validBulkPublicationIntent(&complete) {
		t.Fatal("valid identity rejected")
	}
	for index := range 5 {
		intent := complete
		fields := []*string{&intent.BulkID, &intent.JobID, &intent.WorkerID, &intent.ValidationID, &intent.SourceSnapshotID}
		*fields[index] = ""
		if validBulkPublicationIntent(&intent) {
			t.Fatalf("missing field %d accepted", index)
		}
	}
}
