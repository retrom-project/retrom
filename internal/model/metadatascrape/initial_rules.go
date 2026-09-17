package metadatascrape

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func SelectInitialCandidate(candidates []InitialCandidate) (InitialCandidate, bool) {
	if len(candidates) == 0 {
		return InitialCandidate{}, false
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if CompareInitialCandidate(candidate, best) < 0 {
			best = candidate
		}
	}
	return best, true
}

func CompareInitialCandidate(left, right InitialCandidate) int {
	if order := cmp.Compare(right.HitCount, left.HitCount); order != 0 {
		return order
	}
	if order := cmp.Compare(left.FirstQueryOrder, right.FirstQueryOrder); order != 0 {
		return order
	}
	if order := cmp.Compare(left.ProviderGameID, right.ProviderGameID); order != 0 {
		return order
	}
	return cmp.Compare(left.ID, right.ID)
}

type initialReviewMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Developer   string `json:"developer"`
	Publisher   string `json:"publisher"`
	Genre       string `json:"genre"`
	Players     *int   `json:"players"`
	ReleaseYear *int   `json:"releaseYear"`
}

func MergeInitialReviewMetadata(currentJSON, candidateJSON string) (string, string, error) {
	var current, candidate initialReviewMetadata
	if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
		return "", "", fmt.Errorf("decode initial review metadata: %w", err)
	}
	if err := json.Unmarshal([]byte(candidateJSON), &candidate); err != nil {
		return "", "", fmt.Errorf("decode scrape candidate metadata: %w", err)
	}
	if strings.TrimSpace(candidate.Title) != "" {
		current.Title = candidate.Title
	}
	if strings.TrimSpace(candidate.Description) != "" {
		current.Description = candidate.Description
	}
	if strings.TrimSpace(candidate.Developer) != "" {
		current.Developer = candidate.Developer
	}
	if strings.TrimSpace(candidate.Publisher) != "" {
		current.Publisher = candidate.Publisher
	}
	if strings.TrimSpace(candidate.Genre) != "" {
		current.Genre = candidate.Genre
	}
	if candidate.Players != nil {
		current.Players = candidate.Players
	}
	if candidate.ReleaseYear != nil {
		current.ReleaseYear = candidate.ReleaseYear
	}
	merged, err := json.Marshal(current)
	if err != nil {
		return "", "", fmt.Errorf("encode initial review metadata: %w", err)
	}
	return string(merged), current.Title, nil
}

func SelectInitialAssets(change *InitialDraftChange, assets []InitialAsset) {
	assets = slices.Clone(assets)
	slices.SortFunc(assets, func(left, right InitialAsset) int {
		if order := cmp.Compare(left.Ordinal, right.Ordinal); order != 0 {
			return order
		}
		return cmp.Compare(left.ID, right.ID)
	})
	for _, asset := range assets {
		switch asset.Kind {
		case "COVER":
			if change.CoverID == nil {
				change.CoverID = &asset.ID
			}
		case "BACKGROUND":
			if change.BackgroundID == nil {
				change.BackgroundID = &asset.ID
			}
		case "SCREENSHOT":
			change.Screenshots = append(change.Screenshots, asset)
		}
	}
}

func InitialProgress(item InitialImport, now int64) InitialProgressChange {
	return InitialProgressChange{
		ItemID:          item.ItemID,
		ImportJobID:     item.ImportJobID,
		ExpectedRunning: item.Running,
		ExpectedVersion: item.Version,
		Now:             now,
	}
}

func MetadataRetryDelay(attempt int64) int64 {
	delays := [...]int64{1000, 5000, 30000, 120000}
	return delays[max(0, min(attempt-1, int64(len(delays)-1)))]
}
