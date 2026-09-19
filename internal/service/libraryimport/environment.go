package libraryimport

import (
	"time"

	"retrom/internal/capability/engine/scummvm"
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
	ScummVMDetector                            *scummvm.Detector
}

type MultiDiscAttachmentOptions struct {
	Now              func() time.Time
	StorageAvailable bool
}
