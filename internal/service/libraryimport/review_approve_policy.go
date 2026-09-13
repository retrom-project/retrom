package libraryimport

import (
	"math"
	"sort"
	"strings"
)

func normalizeReviewApproval(request ReviewApprovalRequest) (ReviewApprovalRequest, error) {
	if request.ItemID == "" || request.ExpectedVersion < 1 || request.ExpectedVersion == math.MaxInt64 {
		return ReviewApprovalRequest{}, ErrInvalid
	}
	decision := &request.Decision
	if decision.Reason != nil {
		reason := strings.TrimSpace(*decision.Reason)
		if reason == "" || !validField(reason, 500, true) {
			return ReviewApprovalRequest{}, ErrInvalid
		}
		decision.Reason = &reason
	}
	if !validApprovalDecision(*decision) || !validBulkPublicationIntent(request.Bulk) {
		return ReviewApprovalRequest{}, ErrInvalid
	}
	return request, nil
}

func validApprovalDecision(decision ReviewApprovalDecision) bool {
	if decision.DuplicatePolicy != "" && decision.DuplicatePolicy != "ALLOW_NEW" {
		return false
	}
	if decision.DuplicatePolicy == "" && len(decision.AcknowledgedGameIDs) != 0 {
		return false
	}
	if decision.SourceKind != "" {
		if !ValidApprovalSourceKind(decision.SourceKind) || decision.SourceRefID == "" {
			return false
		}
	} else if decision.SourceRefID != "" || len(decision.ExternalAssets) != 0 {
		return false
	}
	return ValidApprovalExternalAssets(decision.ExternalAssets)
}

func validBulkPublicationIntent(intent *BulkPublicationIntent) bool {
	return intent == nil || (intent.BulkID != "" && intent.JobID != "" && intent.WorkerID != "" &&
		intent.ValidationID != "" && intent.SourceSnapshotID != "")
}

func ValidApprovalSourceKind(value string) bool {
	return value == "SERVER_PEGASUS_IMPORT" || value == "SERVER_EMULATIONSTATION_IMPORT"
}

func ValidApprovalExternalAssets(assets []ApprovalExternalAsset) bool {
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if _, exists := seen[asset.Kind]; exists || asset.BlobID == "" || !ValidApprovalExternalAsset(asset) {
			return false
		}
		seen[asset.Kind] = struct{}{}
	}
	return true
}

func ValidApprovalExternalAsset(asset ApprovalExternalAsset) bool {
	switch asset.Kind {
	case "COVER":
		return asset.WidthPX != nil && asset.HeightPX != nil && *asset.WidthPX > 0 && *asset.HeightPX > 0 &&
			(asset.MediaType == "image/png" || asset.MediaType == "image/jpeg" ||
				asset.MediaType == "image/webp")
	case "VIDEO":
		return asset.WidthPX == nil && asset.HeightPX == nil &&
			(asset.MediaType == "video/mp4" || asset.MediaType == "video/webm")
	default:
		return false
	}
}

func ApprovalDuplicateIDs(games []DuplicateGame) []string {
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.GameID)
	}
	sort.Strings(ids)
	return ids
}

func SameApprovalDuplicateIDs(games []DuplicateGame, acknowledged []string) bool {
	if len(games) != len(acknowledged) {
		return false
	}
	want, got := ApprovalDuplicateIDs(games), append([]string(nil), acknowledged...)
	sort.Strings(got)
	for index := range want {
		if want[index] != got[index] || (index > 0 && got[index] == got[index-1]) {
			return false
		}
	}
	return true
}
