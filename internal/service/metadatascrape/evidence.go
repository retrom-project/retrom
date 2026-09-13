package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/adapter/metadata/hasheous"
)

type WorkerEvidence struct {
	ID          string
	Hashes      Hashes
	Attempts    int
	LastOutcome hasheous.ProviderOutcome
	LastSource  string
}
type EvidenceProgress struct {
	Items          []WorkerEvidence
	CandidateCount int
}
type EvidenceReader interface {
	Evidence(context.Context, string) (EvidenceProgress, error)
}
type EvidenceLookup interface {
	Lookup(context.Context, hasheous.ContentHashes, bool) (ResolvedLookup, error)
}
type EvidenceResults interface {
	Record(context.Context, LookupAttempt) (bool, error)
}
type EvidenceProcessor struct {
	evidence EvidenceReader
	lookup   EvidenceLookup
	results  EvidenceResults
}

func NewProcessor(
	evidence EvidenceReader,
	lookup EvidenceLookup,
	results EvidenceResults,
) *EvidenceProcessor {
	return &EvidenceProcessor{evidence: evidence, lookup: lookup, results: results}
}

func (processor *EvidenceProcessor) Process(
	ctx context.Context,
	claim WorkerClaim,
	payloadJSON string,
) (int, string, error) {
	var payload struct {
		BypassCache bool `json:"bypassCache"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return 0, "METADATA_JOB_PAYLOAD_INVALID", fmt.Errorf("metadata_job_payload_invalid: %w", err)
	}
	evidence, err := processor.evidence.Evidence(ctx, claim.RunID)
	if err != nil {
		return 0, "METADATA_EVIDENCE_FAILED", fmt.Errorf("metadata_evidence_failed: %w", err)
	}
	count, code, err := processor.processScrapeEvidence(ctx, claim, evidence, payload.BypassCache)
	if err != nil {
		return count, code, err
	}
	return count, "", nil
}
