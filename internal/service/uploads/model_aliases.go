package uploads

import model "retrom/internal/model/uploads"

type (
	BlobRecords         = model.BlobRecords
	BlobWriter          = model.BlobWriter
	BrokenPart          = model.BrokenPart
	Candidate           = model.Candidate
	Canceled            = model.Canceled
	CreateRequest       = model.CreateRequest
	File                = model.File
	FileDeclaration     = model.FileDeclaration
	FileKey             = model.FileKey
	FileProgress        = model.FileProgress
	FilePublication     = model.FilePublication
	Finalization        = model.Finalization
	FinalizationInput   = model.FinalizationInput
	FinalizationRecords = model.FinalizationRecords
	FileRecords         = model.FileRecords
	FrozenFile          = model.FrozenFile
	Job                 = model.Job
	JobCancellation     = model.JobCancellation
	JobClaim            = model.JobClaim
	JobCreation         = model.JobCreation
	JobFinish           = model.JobFinish
	JobRecords          = model.JobRecords
	LeaseRecords        = model.LeaseRecords
	Part                = model.Part
	PartRecord          = model.PartRecord
	PartRecords         = model.PartRecords
	PartTarget          = model.PartTarget
	PendingFailure      = model.PendingFailure
	Registration        = model.Registration
	Repository          = model.Repository
	Run                 = model.Run
	Session             = model.Session
	SessionFinish       = model.SessionFinish
	SessionProgress     = model.SessionProgress
	SessionRecords      = model.SessionRecords
	SessionState        = model.SessionState
	WriteScope          = model.WriteScope
)

const PartSize = model.PartSize

var (
	ErrInvalid  = model.ErrInvalid
	ErrNotFound = model.ErrNotFound
)
