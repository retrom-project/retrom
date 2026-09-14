package importdiscard

import model "retrom/internal/model/importdiscard"

type (
	Batch          = model.Batch
	Disposition    = model.Disposition
	Envelope       = model.Envelope
	EnvelopeFile   = model.EnvelopeFile
	ImportWorkflow = model.ImportWorkflow
	Key            = model.Key
	Ownership      = model.Ownership
	Progress       = model.Progress
	Reader         = model.Reader
	ReleaseFacts   = model.ReleaseFacts
	Repository     = model.Repository
	Request        = model.Request
	RequestWriter  = model.RequestWriter
	SourceWorkflow = model.SourceWorkflow
	SourceWriter   = model.SourceWriter
	WriteScope     = model.WriteScope
)

var (
	ErrInvalid        = model.ErrInvalid
	ErrNotFound       = model.ErrNotFound
	ErrReleaseFailed  = model.ErrReleaseFailed
	ErrAmbiguousOwner = model.ErrAmbiguousOwner
	ErrNotCancellable = model.ErrNotCancellable
)
