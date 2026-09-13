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
	WithMove(context.Context, func(MoveScope) error) error
}

// MoveScope contains writes that must be committed together with the move
// audit event.
type MoveScope interface {
	UpdateGame(context.Context, string, string, int64, int64) (bool, error)
	Audit(context.Context, AuditEvent) error
}

// ValidationResolver supplies the immutable BIOS snapshot used in a move
// impact digest.
type ValidationResolver interface {
	ResolveBIOS(context.Context, string, string, string) (corevalidation.Snapshot, string, string, error)
}
