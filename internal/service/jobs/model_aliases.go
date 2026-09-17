package jobs

import model "retrom/internal/model/jobs"

type (
	Cancellation    = model.Cancellation
	CancelCommand   = model.CancelCommand
	CancelResult    = model.CancelResult
	Event           = model.Event
	EventBatch      = model.EventBatch
	EventQuery      = model.EventQuery
	ImportProgress  = model.ImportProgress
	Job             = model.Job
	ReadRecords     = model.ReadRecords
	Repository      = model.Repository
	Result          = model.Result
	RetryCommand    = model.RetryCommand
	RetryWrite      = model.RetryWrite
	Snapshot        = model.Snapshot
)

const EventBatchSize = model.EventBatchSize

var (
	ErrNotFound       = model.ErrNotFound
	ErrConflict       = model.ErrConflict
	ErrRetryViaDomain = model.ErrRetryViaDomain
)
