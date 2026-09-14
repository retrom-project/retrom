package jobs

import model "retrom/internal/model/jobs"

type (
	Cancellation   = model.Cancellation
	Event          = model.Event
	EventBatch     = model.EventBatch
	EventQuery     = model.EventQuery
	ImportProgress = model.ImportProgress
	Job            = model.Job
	ReadRecords    = model.ReadRecords
	Records        = model.Records
	Repository     = model.Repository
	RetryWrite     = model.RetryWrite
	Snapshot       = model.Snapshot
)

const EventBatchSize = model.EventBatchSize

var (
	ErrNotFound = model.ErrNotFound
	ErrConflict = model.ErrConflict
)
