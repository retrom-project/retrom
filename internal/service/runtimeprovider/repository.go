package runtimeprovider

import "context"

type Repository interface {
	WithWrite(context.Context, func(WriteScope) error) error
}
type WriteScope struct {
	Catalog    CatalogRecords
	Projection ProjectionRecords
}
type CatalogRecords interface {
	Current(context.Context) (CurrentState, error)
	CheckpointFormats(context.Context, TargetIdentity) ([]string, error)
	TargetReferenced(context.Context, TargetIdentity) (bool, error)
}
type ProjectionRecords interface {
	Publish(context.Context, Publication) error
	TerminateSessions(context.Context, string, int64) error
	Audit(context.Context, Audit) error
}
type (
	TargetIdentity struct{ ProviderID, TargetID string }
	CurrentState   struct {
		Providers     map[string]CurrentProvider
		Targets       []TargetIdentity
		CatalogSHA256 string
	}
)

type Publication struct {
	Candidate        Projection
	RemovedTargets   []TargetIdentity
	RemovedProviders []string
	AtMS             int64
}
type Audit struct {
	ID       string
	DiffJSON []byte
	AtMS     int64
}
