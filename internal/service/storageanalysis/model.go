package storageanalysis

import (
	"errors"
	"math"
)

const Scope = "OWNED_FILES_V1"

type CategoryCode string

const (
	CategoryGameContent   CategoryCode = "GAME_CONTENT"
	CategoryBIOS          CategoryCode = "BIOS"
	CategorySaves         CategoryCode = "SAVES"
	CategoryMedia         CategoryCode = "MEDIA"
	CategoryWorkflow      CategoryCode = "WORKFLOW"
	CategoryPendingDelete CategoryCode = "PENDING_DELETE"
)

var categoryOrder = [...]CategoryCode{
	CategoryGameContent,
	CategoryBIOS,
	CategorySaves,
	CategoryMedia,
	CategoryWorkflow,
	CategoryPendingDelete,
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
	RegisteredBytes    int64
	RetainedBytes      int64
	PendingDeleteBytes int64
	FileCount          int64
}

type Category struct {
	Code      CategoryCode
	Bytes     int64
	FileCount int64
}

type SaveStateDetails struct {
	ActiveCount     int64
	DeletedCount    int64
	StateBytes      int64
	ScreenshotBytes int64
}

type CleanupCandidateDetails struct {
	FileCount int64
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

type Usage uint8

const (
	UsageGame Usage = iota + 1
	UsageBIOS
	UsageSaves
	UsageMedia
	UsageWorkflow
)

var (
	errIntegerOverflow = errors.New("STORAGE_ANALYSIS_INTEGER_OVERFLOW")
	ErrOwnerInvalid    = errors.New("STORAGE_ANALYSIS_OWNER_INVALID")
)

func classify(retained bool, usage Usage) (CategoryCode, error) {
	if !retained {
		return CategoryPendingDelete, nil
	}
	switch usage {
	case UsageGame:
		return CategoryGameContent, nil
	case UsageBIOS:
		return CategoryBIOS, nil
	case UsageSaves:
		return CategorySaves, nil
	case UsageMedia:
		return CategoryMedia, nil
	case UsageWorkflow:
		return CategoryWorkflow, nil
	default:
		return "", ErrOwnerInvalid
	}
}

func addChecked(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, errIntegerOverflow
	}
	return left + right, nil
}
