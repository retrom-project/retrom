package gamelist

import "errors"

var (
	ErrInvalid  = errors.New("INVALID_REQUEST")
	ErrNotFound = errors.New("GAME_NOT_FOUND")
)

const (
	SortTitleAsc    = "TITLE_ASC"
	SortAddedDesc   = "ADDED_DESC"
	SortRecentDesc  = "RECENT_DESC"
	SortUpdatedDesc = "UPDATED_DESC"
)

type NamedResource struct {
	ID   string
	Name string
}

type Detail struct {
	GameID, Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                                    *int64
	Platform                                                NamedResource
	PlatformInstance                                        NamedResource
	Version, UpdatedAtMS, ActiveDurationMS                  int64
	CoverAssetID, VideoAssetID                              *string
	CoreOptions                                             []CoreOption
	DOSEntries                                              []DOSEntry
	DefaultDOSEntry                                         *string
	SaveStateCount                                          int64
	SaveStates                                              []SaveState
}

type CoreOption struct {
	CoreID, Name                    string
	IsDefault, RequiresThreads      bool
	Status, RevalidationStatus      string
	VariantID, ProviderID, TargetID *string
	DATVersionID, RevalidationJobID *string
	Reasons                         []Reason
}

type Reason struct {
	Code, Level string
}

type DOSEntry struct {
	Path, OriginalPath, Kind  string
	Rank                      int64
	Enabled, DirectLaunchSafe bool
}

type SaveState struct {
	ID, Name, CoreID, CoreName string
	CreatedAtMS                int64
	LastSyncedAtMS, DiscIndex  *int64
	HasScreenshot              bool
}

type Filters struct {
	Query, TagID, PlatformID, PlatformInstanceID, Status string
}

type Cursor struct {
	SortValues []string
	ID         string
}

type ListRequest struct {
	ProfileID      string
	IncludeDeleted bool
	Filters        Filters
	Sort           string
	Cursor         *Cursor
	Limit          int
	IncludeFacets  bool
}

type GameItem struct {
	ID, Title, Status                       string
	Platform, PlatformInstance, DefaultCore NamedResource
	Version, CreatedAtMS, UpdatedAtMS       int64
	LastPlayedAtMS, ReleaseYear             *int64
	MetadataComplete                        bool
	RuntimeStatus                           *string
	CoverAssetID                            *string
}

type Facet struct {
	ID, Name, PlatformID string
	Count                int64
}

type Facets struct {
	TotalCount        int64
	Platforms         []Facet
	PlatformInstances []Facet
	Tags              []Facet
}

type ListResult struct {
	Items         []GameItem
	NextCursor    *Cursor
	FilteredCount int64
	Facets        Facets
}
