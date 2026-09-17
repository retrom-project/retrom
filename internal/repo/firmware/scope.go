package firmware

import (
	"context"

	"retrom/internal/adapter/files/blobstore"
	fwcap "retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
	fwmodel "retrom/internal/model/firmware"
)

type readScope struct {
	requirements  requirementReader
	uploads       uploadReader
	installations installationReader
	archives      archiveReader
}
type writeScope struct {
	readScope
	archives      archiveWriter
	installations installationWriter
	retirements   supersessionScope
	server        serverRecords
	blobs         blobRecords
}

type requirementReader interface {
	Get(context.Context, string) (fwmodel.Requirement, bool, error)
	DATEntries(context.Context, string) ([]fwcap.ExpectedDATEntry, error)
}
type uploadReader interface {
	Get(context.Context, string) (fwmodel.Upload, bool, error)
}
type installationReader interface {
	Active(context.Context, string) (fwmodel.ActiveInstallation, bool, error)
}
type archiveReader interface {
	Entries(context.Context, string) ([]importing.ArchiveEntry, error)
}
type archiveWriter interface {
	Put(context.Context, string, []importing.ArchiveEntry, int64) error
}
type installationWriter interface {
	Create(context.Context, fwmodel.InstallationWrite) error
	Consume(context.Context, fwmodel.Consumption) error
}
type (
	supersessionScope = fwmodel.SupersessionScope
	serverRecords     interface {
		LockExecution(context.Context, serverExecution) error
		SelectCandidate(context.Context, fwmodel.Selection) error
		Finish(context.Context, fwmodel.ServerOutcome) error
	}
)

type serverExecution struct {
	ImportID, JobID, WorkerID string
	ExecutionNo, AtMS         int64
}
type blobRecords interface {
	Ensure(context.Context, blobstore.Metadata, int64) (string, error)
}
