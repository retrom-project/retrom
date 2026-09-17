package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/metadatascrape"
)

type EvidenceProcessor struct {
	evidence model.EvidenceReader
	lookup   model.EvidenceLookup
	results  model.EvidenceResults
}

func NewProcessor(
	evidence model.EvidenceReader,
	lookup model.EvidenceLookup,
	results model.EvidenceResults,
) *EvidenceProcessor {
	return &EvidenceProcessor{evidence: evidence, lookup: lookup, results: results}
}

func (processor *EvidenceProcessor) Process(
	ctx context.Context,
	claim model.WorkerClaim,
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
