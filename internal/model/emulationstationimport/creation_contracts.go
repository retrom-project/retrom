package emulationstationimport

import "context"

type SelectedRoot struct{ ID, Label, Digest string }

type SourceSelector interface {
	Select(context.Context, string, string) (SelectedRoot, error)
}

type CreationPlan struct {
	Request                                                   CreateRequest
	Root                                                      SelectedRoot
	ImportID, JobID, ExecutionID, AuditID, ActorID, DedupeKey string
	NowMS, ExpiresAtMS                                        int64
	ReleaseYearMax                                            int
}

type CreationRepository interface {
	WithCreate(context.Context, func(CreationWriter) error) error
}

type CreationWriter interface {
	PendingPlans(context.Context) (int, error)
	Insert(context.Context, CreationPlan) (Summary, error)
}
