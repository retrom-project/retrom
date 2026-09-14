package payloadrelease

import "context"

type BlobEdge struct{ Table, Column string }

type LifecycleOwner struct {
	Owner                                         Owner
	ReleaseJobID, ReleaseKind, PublicReleaseJobID string
	ReleaseScope                                  Scope
}

type LifecycleReader interface {
	BlobEdges(context.Context) ([]BlobEdge, error)
	Owners(context.Context, Scope, int) ([]LifecycleOwner, error)
}

type LifecycleRepository interface {
	WithLifecycle(context.Context, func(LifecycleReader) error) error
}
