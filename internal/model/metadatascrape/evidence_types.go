package metadatascrape

import (
	"context"

	metadatamodel "retrom/internal/model/metadata"
)

type WorkerEvidence struct {
	ID          string
	Hashes      Hashes
	Attempts    int
	LastOutcome metadatamodel.ProviderOutcome
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
	Lookup(context.Context, metadatamodel.ContentHashes, bool) (ResolvedLookup, error)
}

type EvidenceResults interface {
	Record(context.Context, LookupAttempt) (bool, error)
}
