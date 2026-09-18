package pegasusimport

import "context"

type CompanionCandidate struct {
	ItemID string
	File   ExecutionFile
}

type CompanionRegistration struct {
	Before    OwnedItem
	Candidate CompanionCandidate
	Blob      VerifiedBlob
	NowMS     int64
}

type CompanionRepository interface {
	LoadCompanionOwner(context.Context, string) (OwnedItem, error)
	LoadDependencies(context.Context, string, string) ([]string, error)
	LoadCandidates(context.Context, ExecutionItem) ([]CompanionCandidate, error)
	CommitCompanionRegistration(context.Context, CompanionRegistration) (string, error)
}
