package metadatascrape

import (
	"context"

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
