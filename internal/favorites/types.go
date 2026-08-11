package favorites

import "errors"

var (
	ErrInvalid                 = errors.New("INVALID_REQUEST")
	ErrGameNotFound            = errors.New("GAME_NOT_FOUND")
	ErrFolderNotFound          = errors.New("FAVORITE_FOLDER_NOT_FOUND")
	ErrFolderNameConflict      = errors.New("FAVORITE_FOLDER_NAME_CONFLICT")
	ErrIdempotencyReused       = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrVersionConflict         = errors.New("RESOURCE_VERSION_CONFLICT")
	ErrBatchTooLarge           = errors.New("FAVORITE_BATCH_TOO_LARGE")
	ErrFolderLimit             = errors.New("FAVORITE_FOLDER_LIMIT_REACHED")
	ErrInvalidCursor           = errors.New("INVALID_CURSOR")
	ErrInvalidFolderName       = errors.New("INVALID_FAVORITE_FOLDER_NAME")
	ErrInvalidFavoriteListSort = errors.New("INVALID_FAVORITE_SORT")
	ErrInvariant               = errors.New("FAVORITE_INVARIANT_VIOLATION")
)

const (
	ScopeAll           = "ALL"
	ScopeUncategorized = "UNCATEGORIZED"
	ScopeFolder        = "FOLDER"

	SortFavoritedDesc     = "FAVORITED_DESC"
	SortRecentlyPlayed    = "RECENTLY_PLAYED_DESC"
	SortTitleAsc          = "TITLE_ASC"
	SortReleaseYearDesc   = "RELEASE_YEAR_DESC"
	MaxFolders            = 100
	MaxOrganizeGames      = 50
	MaxOrganizeFolders    = 20
	MaxOrganizeEdges      = 1000
	MaxUnfavoriteGames    = 100
	MaxRestoreGames       = 100
	MaxRestoreFolderEdges = 1000
)

type Principal struct {
	UserID    string
	ProfileID string
}

type FavoriteReference struct {
	FavoritedAtMS int64    `json:"favoritedAtMs"`
	FolderIDs     []string `json:"folderIds"`
}

type State struct {
	GameID        string   `json:"gameId"`
	FavoritedAtMS int64    `json:"favoritedAtMs"`
	FolderIDs     []string `json:"folderIds"`
}

type Folder struct {
	FolderID         string `json:"folderId"`
	Name             string `json:"name"`
	Version          int64  `json:"version"`
	VisibleGameCount int64  `json:"visibleGameCount"`
	CreatedAtMS      int64  `json:"createdAtMs"`
	UpdatedAtMS      int64  `json:"updatedAtMs"`
}

type Summary struct {
	FavoriteCount      int64 `json:"favoriteCount"`
	UncategorizedCount int64 `json:"uncategorizedCount"`
	FolderCount        int64 `json:"folderCount"`
}

type NamedResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PlatformSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type GameItem struct {
	GameID           string            `json:"gameId"`
	Title            string            `json:"title"`
	Platform         NamedResource     `json:"platform"`
	PlatformInstance NamedResource     `json:"platformInstance"`
	DefaultCore      NamedResource     `json:"defaultCore"`
	CoverURL         *string           `json:"coverUrl"`
	ReleaseYear      *int64            `json:"releaseYear"`
	CreatedAtMS      int64             `json:"createdAtMs"`
	LastPlayedAtMS   *int64            `json:"lastPlayedAtMs"`
	Favorite         FavoriteReference `json:"favorite"`
}

type PageCursor struct {
	SortValues []string
	ID         string
}

type ListOptions struct {
	Scope      string
	FolderID   string
	Query      string
	PlatformID string
	Sort       string
	Limit      int
	Cursor     *PageCursor
}

type ListResult struct {
	GeneratedAtMS int64             `json:"generatedAtMs"`
	Summary       Summary           `json:"summary"`
	Folders       []Folder          `json:"folders"`
	Platforms     []PlatformSummary `json:"platforms"`
	TotalCount    int64             `json:"totalCount"`
	Items         []GameItem        `json:"items"`
	NextCursor    *PageCursor       `json:"-"`
}

type BatchResult struct {
	Items []State `json:"items"`
}

type UnfavoriteItem struct {
	GameID    string   `json:"gameId"`
	FolderIDs []string `json:"folderIds"`
}

type UnfavoriteResult struct {
	Items []UnfavoriteItem `json:"items"`
}

type RestoreItem struct {
	GameID    string   `json:"gameId"`
	FolderIDs []string `json:"folderIds"`
}

type RestoreResult struct {
	RestoredGameIDs  []string `json:"restoredGameIds"`
	SkippedGameIDs   []string `json:"skippedGameIds"`
	SkippedFolderIDs []string `json:"skippedFolderIds"`
}

type IdempotentResponse struct {
	Status   int
	Headers  map[string]string
	Body     []byte
	Replayed bool
}
