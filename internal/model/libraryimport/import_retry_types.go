package libraryimport

import "context"

// ImportItemRetryRequest identifies one failed item that may be queued again.
// The version is an optimistic concurrency fence owned by the application
// service; storage only persists the resulting transition.
type ImportItemRetryRequest struct {
	ItemID          string
	ExpectedVersion int64
}

type ImportItemRetryResult struct {
	ItemID  string
	JobID   string
	State   string
	Version int64
}

type ImportItemRetrySnapshot struct {
	ImportID       string
	Stage          string
	ManifestDigest string
	Version        int64
	State          string
}

type ImportItemRetryWrite struct {
	ItemID          string
	ImportID        string
	Stage           string
	ManifestDigest  string
	ExpectedVersion int64
	JobID           string
	DedupeKey       string
	PayloadJSON     string
	NowMS           int64
}

type ImportItemRetryRepository interface {
	LoadRetrySnapshot(context.Context, string) (ImportItemRetrySnapshot, bool, error)
	CommitRetry(context.Context, ImportItemRetryWrite) error
}
