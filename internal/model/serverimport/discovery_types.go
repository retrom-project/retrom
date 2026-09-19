package serverimport

import (
	"context"
)

type DiscoveryCounts struct {
	Directories            int64
	Files                  int64
	SkippedSpecial         int64
	SkippedUnrepresentable int64
}

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
	Counts               DiscoveryCounts
}

type DiscoveryRecords interface {
	Reset(context.Context, Work, int64) error
	Persist(context.Context, DiscoveryPlan) error
}

type DiscoveryRepository interface {
	WithWrite(context.Context, func(DiscoveryRecords) error) error
}
