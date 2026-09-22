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

type Repository interface {
	WithRead(context.Context, func(Reader) error) error
	WithWrite(context.Context, func(WriteScope) error) error
}
type Reader interface {
	Batch(context.Context, Key) (Batch, error)
	Disposition(context.Context, Key) (Disposition, bool, error)
	Pending(context.Context) (Request, bool, error)
	Children(context.Context, Key) ([]string, error)
}
type WriteScope struct {
	Reader
	Requests RequestWriter
	Sources  SourceWriter
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
type ImportWorkflow interface {
	CancelForDiscard(context.Context, string, int64) error
	DiscardBatchReviews(context.Context, string) (bool, error)
	ReleaseDiscardedBatch(context.Context, string) error
}
type SourceWorkflow interface {
	Cancel(context.Context, string, string, int64, string, string) error
}
