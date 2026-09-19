package libraryimport

import "time"

// ImportAdmissionOptions contains service-level configuration for import
// admission. Moved from model because it carries func fields.
type ImportAdmissionOptions struct {
	Now                                        func() time.Time
	MultiDiscEnabled, MetadataScraperAvailable bool
}

// ImportCreationSettings contains service-level configuration for import
// creation. Moved from model because it carries func fields.
type ImportCreationSettings struct {
	Now              func() time.Time
	MultiDiscEnabled bool
}

// ImportWorkerSettings contains service-level configuration for import
// workers. Moved from model because it carries func fields.
type ImportWorkerSettings struct {
	Now    func() time.Time
	Report func(error)
}

// MultiDiscAttachmentOptions contains service-level configuration for
// multi-disc attachment. Moved from model because it carries func fields.
type MultiDiscAttachmentOptions struct {
	Now              func() time.Time
	StorageAvailable bool
}
