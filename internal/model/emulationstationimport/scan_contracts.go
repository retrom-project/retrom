package emulationstationimport

import "context"

type ScanMutation struct {
	Before LeaseSnapshot
	NowMS  int64
}

type ScanSnapshotReader interface {
	LoadScanOwner(context.Context, string) (LeaseSnapshot, bool, error)
}

type ScanCommitter interface {
	CommitScanClear(context.Context, ScanMutation) error
	CommitScanHeaders(context.Context, ScanMutation, ScanProjection) error
	CommitScanItems(context.Context, ScanMutation, []ScanItem) error
	CommitScanComplete(context.Context, ScanMutation, ScanProjection) error
	CommitScanRejection(context.Context, ScanMutation, ScanProjection) error
}

type ScanRepository interface {
	ScanSnapshotReader
	ScanCommitter
}
