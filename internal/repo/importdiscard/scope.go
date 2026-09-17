package importdiscard

import (
	"context"

	model "retrom/internal/model/importdiscard"
)

type reader interface {
	Batch(context.Context, model.Key) (model.Batch, error)
	Disposition(context.Context, model.Key) (model.Disposition, bool, error)
	Pending(context.Context) (model.Request, bool, error)
	Children(context.Context, model.Key) ([]string, error)
}
type writeScope struct {
	reader    reader
	requests  requestWriter
	sources   sourceWriter
	ownership ownership
}
type requestWriter interface {
	Request(context.Context, model.Request) error
	Progress(context.Context, model.Progress) error
}
type sourceWriter interface {
	Releases(context.Context, model.Key) (model.ReleaseFacts, error)
	Complete(context.Context, model.Key, int64) error
	UnusedUploads(context.Context, model.Key) ([]string, error)
	DeleteUpload(context.Context, string) error
}
type ownership interface {
	Unlinked(context.Context, model.Key) ([]string, error)
	ImportByUpload(context.Context, string) (string, error)
	LegacyCandidates(context.Context, string) ([]model.Envelope, error)
	OwnerCount(context.Context, string) (int64, error)
	Link(context.Context, string, string, string) error
}
