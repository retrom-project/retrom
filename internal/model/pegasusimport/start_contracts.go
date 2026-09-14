package pegasusimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

const MaxMetadataFiles = 1000

type MetadataEvidence struct {
	RelativePath               string
	SizeBytes                  int64
	ContentDigest, FactsDigest string
	ParseState, ErrorCode      string
}

type StartSnapshot struct {
	Summary                                Summary
	RootConfigDigest, SourceSnapshotDigest string
	Metadata                               []MetadataEvidence
	TagsValid, OtherActive                 bool
}

type StartScope struct {
	Payload payload.ReleaseScope
	Read    StartReader
	Write   StartWriter
}

type StartReader interface {
	Current(context.Context, string) (StartSnapshot, error)
}

type StartWriter interface {
	Queue(context.Context, StartPlan) error
}

type StartRepository interface {
	Inspect(context.Context, string) (StartSnapshot, error)
	WithStart(context.Context, func(StartScope) error) error
}

type StartSources interface {
	Select(context.Context, string, string) (SelectedRoot, error)
	VerifyMetadata(context.Context, string, string, []MetadataEvidence) error
}

type StartPlan struct {
	Before                                          StartSnapshot
	JobID, ExecutionID, AuditID, ActorID, DedupeKey string
	NowMS                                           int64
}
