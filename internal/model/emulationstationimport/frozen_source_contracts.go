package emulationstationimport

import "context"

const (
	MaxSnapshotGamelists            = 1000
	MaxSnapshotGamelistBytes  int64 = 8 << 20
	MaxSnapshotGamelistsBytes int64 = 64 << 20
)

type GamelistEvidence struct {
	RelativePath, FactsDigest, ParseState string
	ContentDigest                         *string
	SizeBytes                             int64
}

type FrozenSourceSnapshot struct {
	RootConfigDigest, SourceSnapshotDigest string
	ReleaseYearMax                         int
	Gamelists                              []GamelistEvidence
}

type FrozenSources interface {
	Select(context.Context, string, string) (SelectedRoot, error)
	VerifyGamelists(context.Context, string, string, []GamelistEvidence) error
}
