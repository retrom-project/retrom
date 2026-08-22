package storageanalysis

import (
	"errors"
	"math"
)

const Scope = "REGISTERED_CAS_PAYLOAD_V1"

type CategoryCode string

const (
	CategoryGameContent     CategoryCode = "GAME_CONTENT"
	CategoryBIOS            CategoryCode = "BIOS"
	CategorySaves           CategoryCode = "SAVES"
	CategoryMedia           CategoryCode = "MEDIA"
	CategoryWorkflow        CategoryCode = "WORKFLOW"
	CategoryRuntimeSnapshot CategoryCode = "RUNTIME_SNAPSHOT"
	CategorySharedDurable   CategoryCode = "SHARED_DURABLE"
	CategoryOtherReferenced CategoryCode = "OTHER_REFERENCED"
	CategoryUnreferenced    CategoryCode = "UNREFERENCED"
)

var categoryOrder = [...]CategoryCode{
	CategoryGameContent,
	CategoryBIOS,
	CategorySaves,
	CategoryMedia,
	CategoryWorkflow,
	CategoryRuntimeSnapshot,
	CategorySharedDurable,
	CategoryOtherReferenced,
	CategoryUnreferenced,
}

var Excluded = [...]string{
	"DATABASE_FILES",
	"UPLOAD_PARTS",
	"JOB_SCRATCH",
	"DEPENDENCY_ROOT",
	"FILESYSTEM_OVERHEAD",
	"UNREGISTERED_ORPHANS",
	"VOLUME_FREE_SPACE",
}

type Totals struct {
	RegisteredBytes   int64
	ProtectedBytes    int64
	UnreferencedBytes int64
	BlobCount         int64
}

type Category struct {
	Code      CategoryCode
	Bytes     int64
	BlobCount int64
}

type SaveStateDetails struct {
	ActiveCount              int64
	DeletedCount             int64
	StateReferenceBytes      int64
	ScreenshotReferenceBytes int64
}

type CleanupCandidateDetails struct {
	BlobCount int64
	Bytes     int64
}

type Details struct {
	SaveStates        SaveStateDetails
	CleanupCandidates CleanupCandidateDetails
}

type Snapshot struct {
	Scope         string
	GeneratedAtMS int64
	Totals        Totals
	Categories    []Category
	Details       Details
	Excluded      []string
}

type usage uint8

const (
	usageGame usage = 1 << iota
	usageBIOS
	usageSaves
	usageMedia
	usageWorkflow
	usageRuntime
)

const durableUsage = usageGame | usageBIOS | usageSaves | usageMedia

var errIntegerOverflow = errors.New("STORAGE_ANALYSIS_INTEGER_OVERFLOW")

func classify(protected bool, flags usage) CategoryCode {
	if !protected {
		return CategoryUnreferenced
	}
	durable := flags & durableUsage
	if durable != 0 && durable&(durable-1) != 0 {
		return CategorySharedDurable
	}
	switch {
	case durable&usageGame != 0:
		return CategoryGameContent
	case durable&usageBIOS != 0:
		return CategoryBIOS
	case durable&usageSaves != 0:
		return CategorySaves
	case durable&usageMedia != 0:
		return CategoryMedia
	case flags&usageWorkflow != 0:
		return CategoryWorkflow
	case flags&usageRuntime != 0:
		return CategoryRuntimeSnapshot
	default:
		return CategoryOtherReferenced
	}
}

func addChecked(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, errIntegerOverflow
	}
	return left + right, nil
}
