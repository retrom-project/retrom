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

type CompanionReader interface {
	Owner(context.Context, string) (OwnedItem, error)
	Dependencies(context.Context, string, string) ([]string, error)
	Candidates(context.Context, ExecutionItem) ([]CompanionCandidate, error)
}

type CompanionWriter interface {
	Register(context.Context, CompanionRegistration) (string, error)
}

type CompanionScope struct {
	Read  CompanionReader
	Write CompanionWriter
}

type CompanionRepository interface {
	WithCompanions(context.Context, func(CompanionScope) error) error
}
