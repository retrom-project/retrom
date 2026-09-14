package gamecontent

// AdminGameDetail is the database-independent projection returned by the
// administrative game detail use case.
type AdminGameDetail struct {
	Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                            *int64
	Status, PayloadState                            string
	PayloadReleaseJobID, PayloadLastErrorCode       *string
	InstanceID, InstanceName, PlatformID            string
	ContentKind                                     string
	Version, CreatedAtMS, UpdatedAtMS               int64
	DeletedAtMS                                     *int64
	Files                                           []AdminGameFile
	Assets                                          []AdminGameAsset
	Variants                                        []AdminGameVariant
}

type AdminGameFile struct {
	Role, LogicalName, SHA256 string
	SortOrder, SizeBytes      int64
}

type AdminGameAsset struct {
	ID, Kind, MediaType string
	Ordinal             int64
	WidthPX, HeightPX   *int64
}

type AdminGameVariant struct {
	ID, CoreID, CoreName, Status, CompatibilityCode string
	ProviderID, TargetID, DATVersionID              *string
	DependencySnapshot                              map[string]any
	Version, CreatedAtMS, UpdatedAtMS               int64
}

type AdminGamePatchState struct {
	Status   string
	Version  int64
	Metadata AdminGameMetadata
}

type AdminGameMetadata struct {
	Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                            *int64
}

type AdminGamePatchRequest struct {
	GameID             string
	ExpectedVersion    int64
	Title              *string
	Description        *string
	Developer          *string
	Publisher          *string
	Genre              *string
	PlayersPresent     bool
	Players            *int64
	ReleaseYearPresent bool
	ReleaseYear        *int64
	Actor              AuditActor
	NowMS              int64
}

type AdminGamePatchUpdate struct {
	GameID          string
	ExpectedVersion int64
	Metadata        AdminGameMetadata
	Actor           AuditActor
	NowMS           int64
}

type AdminGamePatchResult struct {
	Version     int64
	UpdatedAtMS int64
}

type AuditActor struct {
	Kind      string
	UserID    any
	Label     any
	RequestID any
}
