package importdiscard

import model "retrom/internal/model/importdiscard"

type (
	Batch                      = model.Batch
	DiscardSourceItemsCommand  = model.DiscardSourceItemsCommand
	Disposition                = model.Disposition
	Envelope                   = model.Envelope
	EnvelopeFile               = model.EnvelopeFile
	ImportWorkflow             = model.ImportWorkflow
	Key                        = model.Key
	Ownership                  = model.Ownership
	Progress                   = model.Progress
	Reader                     = model.Reader
	RecoverOwnershipCommand    = model.RecoverOwnershipCommand
	ReleaseFacts               = model.ReleaseFacts
	Repository                 = model.Repository
	Request                    = model.Request
	RequestDiscardCommand      = model.RequestDiscardCommand
	RequestWriter              = model.RequestWriter
	SourceWorkflow             = model.SourceWorkflow
	SourceWriter               = model.SourceWriter
	Status                     = model.Status
	WriteScope                 = model.WriteScope
)

var (
	ErrInvalid        = model.ErrInvalid
	ErrNotFound       = model.ErrNotFound
	ErrReleaseFailed  = model.ErrReleaseFailed
	ErrAmbiguousOwner = model.ErrAmbiguousOwner
	ErrNotCancellable = model.ErrNotCancellable
)
