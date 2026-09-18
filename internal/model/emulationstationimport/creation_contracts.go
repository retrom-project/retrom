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
	LoadPendingPlanCount(context.Context) (int, error)
	CommitCreation(context.Context, CreationPlan) (Summary, error)
}
