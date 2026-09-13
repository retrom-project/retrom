package bios

import "errors"

var ErrInvalid = errors.New("INVALID_QUERY")

const (
	ScopeRequiredByLibrary = "REQUIRED_BY_LIBRARY"
	ScopeFullCatalog       = "FULL_CATALOG"
	QuickAll               = "ALL"
	QuickAttention         = "ATTENTION"
	QuickRequired          = "REQUIRED"
	QuickOptional          = "OPTIONAL"
)

type Cursor struct {
	SortValues []string
	ID         string
}

type ListRequest struct {
	Scope      string
	Query      string
	PlatformID string
	CoreID     string
	ProviderID string
	TargetID   string
	Status     string
	Quick      string
	Limit      int
	Cursor     *Cursor
}

type Installation struct {
	ID, MD5, SHA1, SHA256       string
	ValidatedRequirementVersion int64
	CreatedAtMS                 int64
}

type Item struct {
	ID, CoreID, CoreName, ProviderID, TargetID string
	LogicalName, SourceKind, FileKind          string
	RequirementMode, Status                    string
	ConditionCode, ExpectedMD5                 *string
	Enabled                                    bool
	Version                                    int64
	ActiveInstallation                         *Installation
}

type ScopeCounts struct {
	RequiredByLibrary int64
	FullCatalog       int64
}

type Summary struct {
	TotalCount, BlockingCount, WarningCount, ReadyCount int64
	AttentionCount, RequiredCount, OptionalCount        int64
}

type ListResult struct {
	ScopeCounts   ScopeCounts
	Summary       Summary
	FilteredCount int64
	Items         []Item
	NextCursor    *Cursor
}
