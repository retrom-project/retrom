package serverimport

import (
	"context"

	"retrom/internal/adapter/files/serversource"
)

type CandidateWrite struct {
	Evidence      CandidateEvidence
	ExactBasename bool
	Rank          *int64
	NotSelected   *string
	Details       []byte
}

type DiscoveryGroup struct {
	RequirementID string
	Candidates    []CandidateWrite
}

type DiscoveryPlan struct {
	Unit                 Work
	Groups               []DiscoveryGroup
	Total, Multiple, Now int64
	Counts               serversource.Counts
}

type DiscoveryRecords interface {
	Reset(context.Context, Work, int64) error
	Persist(context.Context, DiscoveryPlan) error
}

type DiscoveryRepository interface {
	CommitWrite(context.Context, func(DiscoveryRecords) error) error
}
