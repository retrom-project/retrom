package payloadrelease

import "context"

type GarbageFacts struct {
	Blob                    GCBlob
	Found, OtherDigestOwner bool
	ArchiveEntries          int64
}

type GarbageReader interface {
	Facts(context.Context, string, string) (GarbageFacts, error)
}

type GarbageWriter interface {
	Remove(context.Context, GarbageFacts) error
	Cancel(context.Context, GarbageFacts) error
}

type GarbageScope struct {
	Read   GarbageReader
	Write  GarbageWriter
	Worker WorkerScope
}

type GarbageRepository interface {
	WithGarbage(context.Context, func(GarbageScope) error) error
}

type EffectAuthority interface {
	CheckInScope(context.Context, WorkerScope, Work) error
}

type GarbageFiles interface {
	Delete(context.Context, string) error
}
