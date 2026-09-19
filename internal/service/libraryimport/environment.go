package libraryimport

import (
	"time"

	librarymodel "retrom/internal/model/libraryimport"
)

type ImportWorkerSettings struct {
	Now    func() time.Time
	Report func(error)
}

type ImportCreationSettings struct {
	Now              func() time.Time
	MultiDiscEnabled bool
}

type ImportAdmissionOptions struct {
	Now                                        func() time.Time
	MultiDiscEnabled, MetadataScraperAvailable bool
}

type ImportPreparationOptions struct {
	MultiDiscEnabled, MetadataScraperAvailable bool
	ScummVMDetector                            librarymodel.ScummVMDetector
}

type MultiDiscAttachmentOptions struct {
	Now              func() time.Time
	StorageAvailable bool
}
