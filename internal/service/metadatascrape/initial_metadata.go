package metadatascrape

import (
	"encoding/json"
	"fmt"
	"strings"
)

type initialReviewMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Developer   string `json:"developer"`
	Publisher   string `json:"publisher"`
	Genre       string `json:"genre"`
	Players     *int   `json:"players"`
	ReleaseYear *int   `json:"releaseYear"`
}

func mergeInitialReviewMetadata(currentJSON, candidateJSON string) (string, string, error) {
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
