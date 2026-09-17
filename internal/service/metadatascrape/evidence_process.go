package metadatascrape

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/metadatascrape"

	"retrom/internal/adapter/metadata/hasheous"
)

func hashString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (processor *EvidenceProcessor) processScrapeEvidence(
	ctx context.Context,
	claim model.WorkerClaim,
	evidence model.EvidenceProgress,
	bypassCache bool,
) (int, string, error) {
	candidateCount := evidence.CandidateCount
	for _, item := range evidence.Items {
		if evidenceTerminal(item) {
			continue
		}
		created, code, err := processor.processEvidenceItem(ctx, claim, item, bypassCache, candidateCount < 20)
		if err != nil {
			return 0, code, err
		}
		if created {
			candidateCount++
		}
	}
	return candidateCount, "", nil
}

func (processor *EvidenceProcessor) processEvidenceItem(
	ctx context.Context,
	claim model.WorkerClaim,
	item model.WorkerEvidence,
	bypassCache, allowCandidate bool,
) (bool, string, error) {
	hashes := hasheous.ContentHashes{
		CRC32: hashString(
			item.Hashes.CRC32,
		), MD5: hashString(
			item.Hashes.MD5,
		), SHA1: hashString(
			item.Hashes.SHA1,
		), SHA256: hashString(
			item.Hashes.SHA256,
		),
	}
	for attempt := item.Attempts + 1; attempt <= 3; attempt++ {
		resolved, err := processor.lookup.Lookup(ctx, hashes, bypassCache)
		if err != nil {
			return false, "METADATA_REQUEST_INVALID", fmt.Errorf("metadata_request_invalid: %w", err)
		}
		created, err := processor.results.Record(
			ctx,
			model.LookupAttempt{
				Claim:          claim,
				EvidenceID:     item.ID,
				Lookup:         resolved,
				AttemptNo:      attempt,
				AllowCandidate: allowCandidate,
			},
		)
		if err != nil {
			return false, "METADATA_PERSIST_FAILED", fmt.Errorf("metadata_persist_failed: %w", err)
		}
		if resolved.CachedResponseID != "" || !retryableOutcome(resolved.Result.Outcome) || attempt == 3 {
			return created, "", nil
		}
		if err := waitRetry(ctx, time.Duration(100*(1<<(attempt-1)))*time.Millisecond); err != nil {
			return false, "METADATA_CANCELLED", err
		}
	}
	return false, "", nil
}

func retryableOutcome(outcome hasheous.ProviderOutcome) bool {
	return outcome == hasheous.OutcomeRateLimited || outcome == hasheous.OutcomeTimeout ||
		outcome == hasheous.OutcomeNetworkError
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("metadatascrape/service: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func evidenceTerminal(item model.WorkerEvidence) bool {
	return item.Attempts > 0 && (item.LastSource == "CACHE" || !retryableOutcome(item.LastOutcome) || item.Attempts >= 3)
}
