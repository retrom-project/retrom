package payloadrelease

import "context"

type BlobEdge struct{ Table, Column string }

type LifecycleOwner struct {
	Owner                                         Owner
	ReleaseJobID, ReleaseKind, PublicReleaseJobID string
	ReleaseScope                                  Scope
}

type LifecycleRepository interface {
	BlobEdges(context.Context) ([]BlobEdge, error)
	Owners(context.Context, Scope, int) ([]LifecycleOwner, error)
}
