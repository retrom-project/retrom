package libraryimport

import (
	"time"

	"retrom/internal/model/diagnostics"

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
	ProjectArchives                            librarymodel.ProjectArchiveOpener
	ArchiveInspector                           librarymodel.ArchiveInspector
	Diagnostics                                diagnostics.ErrorReporter
	MultiDiscEnabled, MetadataScraperAvailable bool
	ScummVMDetector                            librarymodel.ScummVMDetector
	ONSDetector                                librarymodel.ONSProjectDetector
	ButterscotchDetector                       librarymodel.ButterscotchProjectDetector
	NXEngineDetector                           librarymodel.NXEngineProjectDetector
	RPGMakerDetector                           librarymodel.RPGMakerDetector
}

type MultiDiscAttachmentOptions struct {
	Now              func() time.Time
	StorageAvailable bool
}
