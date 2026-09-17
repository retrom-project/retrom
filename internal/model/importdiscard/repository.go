package importdiscard

import "context"

type (
	Key   struct{ Kind, ID string }
	Batch struct {
		State                      string
		Version                    int64
		Started                    bool
		Rejected, ResolvedRejected int64
		PayloadState               string
		ItemCounts                 map[string]int64
	}
)

type Disposition struct {
	State     string
	ErrorCode *string
}
type Request struct {
	Key
	UserID, AuditID string
	Now             int64
}
type Progress struct {
	Key
	State       string
	ErrorCode   *string
	CompletedAt *int64
	Now         int64
}
type ReleaseFacts struct{ Releasing, Failed int64 }

// EnvelopeFile preserves the canonical internal upload manifest field names.
type EnvelopeFile struct {
	RelativePath, BlobID string
	SizeBytes            int64
}
type Envelope struct {
	ImportID, Digest string
	Files            []EnvelopeFile
	Complete         bool
}

// RecoverOwnershipCommand captures inputs for recovering source ownership links.
type RecoverOwnershipCommand struct{ Key Key }

// DiscardSourceItemsCommand captures inputs for discarding source items.
type DiscardSourceItemsCommand struct {
	Key   Key
	NowMS int64
}

// RequestDiscardCommand captures inputs for requesting content discard.
type RequestDiscardCommand struct {
	Key    Key
	UserID string
	NowMS  int64
}

type Repository interface { //nolint:interfacebloat // domain groups related named commands
	Batch(context.Context, Key) (Batch, error)
	Disposition(context.Context, Key) (Disposition, bool, error)
	Pending(context.Context) (Request, bool, error)
	Children(context.Context, Key) ([]string, error)
	CommitRecoverOwnership(context.Context, RecoverOwnershipCommand) error
	CommitDiscardSourceItems(context.Context, DiscardSourceItemsCommand) (bool, error)
	CommitRequestDiscard(context.Context, RequestDiscardCommand) (Status, error)
	CommitProgress(context.Context, Progress) error
}

// Status describes the current discard state for a key.
type Status struct {
	Kind      string  `json:"kind"`
	ImportID  string  `json:"importId"`
	State     string  `json:"state"`
	ErrorCode *string `json:"errorCode"`
}
type Reader interface {
	Batch(context.Context, Key) (Batch, error)
	Disposition(context.Context, Key) (Disposition, bool, error)
	Pending(context.Context) (Request, bool, error)
	Children(context.Context, Key) ([]string, error)
}
type WriteScope struct {
	Reader
	Requests  RequestWriter
	Sources   SourceWriter
	Ownership Ownership
}
type RequestWriter interface {
	Request(context.Context, Request) error
	Progress(context.Context, Progress) error
}
type SourceWriter interface {
	Releases(context.Context, Key) (ReleaseFacts, error)
	Complete(context.Context, Key, int64) error
	UnusedUploads(context.Context, Key) ([]string, error)
	DeleteUpload(context.Context, string) error
}
type Ownership interface {
	Unlinked(context.Context, Key) ([]string, error)
	ImportByUpload(context.Context, string) (string, error)
	LegacyCandidates(context.Context, string) ([]Envelope, error)
	OwnerCount(context.Context, string) (int64, error)
	Link(context.Context, string, string, string) error
}
type ImportWorkflow interface {
	CancelForDiscard(context.Context, string, int64) error
	DiscardBatchReviews(context.Context, string) (bool, error)
	ReleaseDiscardedBatch(context.Context, string) error
}
type SourceWorkflow interface {
	Cancel(context.Context, string, string, int64, string, string) error
}
