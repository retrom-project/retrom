package gamemove

import (
	"context"

	corevalidation "retrom/internal/capability/content/corevalidation"
)

// Repository exposes the reads and atomic writes needed by the game move
// application service. Concrete SQL access belongs in persistence.
//
//nolint:interfacebloat // the game move use case intentionally shares one read/write boundary
type Repository interface {
	ImpactSubject(context.Context, string, string) (ImpactSubject, error)
	Variant(context.Context, VariantQuery) (VariantState, bool, error)
	QueuedJobState(context.Context, string) (string, error)
	LatestScrapeRun(context.Context, string) (string, bool, error)
	ScrapeCandidates(context.Context, string) ([]CandidateRecord, error)
	CommitMove(context.Context, MoveCommand) error
}

// MoveCommand carries the values needed for an atomic game move write.
type MoveCommand struct {
	GameID                   string
	TargetPlatformInstanceID string
	ExpectedVersion          int64
	NowMS                    int64
	Audit                    AuditEvent
}

// ValidationResolver supplies the immutable BIOS snapshot used in a move
// impact digest.
type ValidationResolver interface {
	ResolveBIOS(context.Context, string, string, string) (corevalidation.Snapshot, string, string, error)
}
