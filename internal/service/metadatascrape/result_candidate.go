package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"
)

func recordCandidate(
	ctx context.Context,
	scope ResultScope,
	attempt LookupAttempt,
	prepared preparedCandidate,
	responseID, attemptID string,
	now int64,
) (bool, error) {
	id, err := scheduleID()
	if err != nil {
		return false, err
	}
	candidate, err := scope.Write.Candidate(ctx, CandidateRecord{
		ID: id, RunID: attempt.Claim.RunID, ResponseID: responseID,
		ProviderGameID: prepared.value.ProviderGameID,
		MetadataJSON:   prepared.metadata, EvidenceJSON: prepared.evidence, Now: now,
	})
	if err != nil {
		return false, fmt.Errorf("record scrape candidate: %w", err)
	}
	hashes, err := scope.Read.Hashes(ctx, attempt.EvidenceID)
	if err != nil {
		return false, fmt.Errorf("read candidate hash evidence: %w", err)
	}
	encoded, err := matchedHashes(hashes)
	if err != nil {
		return false, err
	}
	if err := scope.Write.Hit(
		ctx,
		CandidateHit{
			CandidateID: candidate.ID,
			AttemptID:   attemptID,
			HashesJSON:  encoded,
			Now:         now,
		},
	); err != nil {
		return false, fmt.Errorf("record candidate hit: %w", err)
	}
	if !candidate.Created {
		return false, nil
	}
	assets := make([]CandidateAsset, 0, len(prepared.value.Assets))
	for _, asset := range prepared.value.Assets {
		id, err := scheduleID()
		if err != nil {
			return false, err
		}
		assets = append(
			assets,
			CandidateAsset{
				ID:          id,
				CandidateID: candidate.ID,
				ResponseID:  responseID,
				Reference:   asset,
				Now:         now,
			},
		)
	}
	for i := range assets {
		if err := enqueueCandidateMedia(ctx, scope, attempt.Claim.RunID, &assets[i]); err != nil {
			return false, err
		}
	}
	if err := scope.Write.Assets(ctx, assets); err != nil {
		return false, fmt.Errorf("record candidate assets: %w", err)
	}
	return true, nil
}

func matchedHashes(hashes Hashes) (string, error) {
	matched := make(map[string]string, 4)
	for name, value := range map[string]*string{
		"crc32":  hashes.CRC32,
		"md5":    hashes.MD5,
		"sha1":   hashes.SHA1,
		"sha256": hashes.SHA256,
	} {
		if value != nil {
			matched[name] = *value
		}
	}
	encoded, err := json.Marshal(matched)
	if err != nil {
		return "", fmt.Errorf("encode matched hashes: %w", err)
	}
	return string(encoded), nil
}
