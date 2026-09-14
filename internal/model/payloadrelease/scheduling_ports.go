package payloadrelease

import "context"

// SourceBatch and SourceReleaseReader are the read-side contracts used by
// payload release orchestration. They contain no workflow implementation.
type SourceBatch struct {
	Type     ScopeType
	ImportID string
}

type SourceReleaseReader interface {
	RetainedSources(context.Context, SourceBatch, string, int) ([]string, error)
	BoundSources(context.Context, string, Scope, int) ([]Scope, error)
}

type ReleaseScope struct {
	Scheduling SchedulingScope
	Links      SourceReleaseReader
}

type ReviewRelease struct {
	ItemID, ImportID string
	Reason           Reason
	NowMS            int64
}
