package model

import "encoding/json"

type (
	Tag struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Version   int64  `json:"version"`
		GameCount int64  `json:"gameCount"`
	}
	Media struct {
		ID         string `json:"id"`
		Kind       string `json:"kind"`
		URL        string `json:"url"`
		MediaType  string `json:"mediaType"`
		Ordinal    int64  `json:"ordinal"`
		StorageKey string `json:"-"`
		SHA256     string `json:"-"`
		SizeBytes  int64  `json:"-"`
	}
)

type GameFile struct {
	ID         string `json:"id"`
	LogicalKey string `json:"logicalKey"`
	Role       string `json:"role"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA256     string `json:"sha256"`
	StorageKey string `json:"-"`
}
type Game struct {
	ID                 string  `json:"id"`
	PlatformInstanceID string  `json:"platformInstanceId"`
	PlatformID         string  `json:"platformId"`
	DirectoryName      string  `json:"directoryName"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	Developer          string  `json:"developer"`
	Publisher          string  `json:"publisher"`
	Genre              string  `json:"genre"`
	Players            *string `json:"players"`
	ReleaseYear        *int64  `json:"releaseYear"`
	Status             string  `json:"status"`
	Source             string  `json:"source"`
	Version            int64   `json:"version"`
	ContentHash        string  `json:"contentHash"`
	Tags               []Tag   `json:"tags"`
	Media              []Media `json:"media"`
	Favorite           bool    `json:"favorite"`
	CreatedAtMs        int64   `json:"createdAtMs"`
	UpdatedAtMs        int64   `json:"updatedAtMs"`
}
type GameDetail struct {
	Game              Game            `json:"game"`
	Files             []GameFile      `json:"files"`
	CoreIDs           []string        `json:"coreIds"`
	DefaultCoreID     string          `json:"defaultCoreId"`
	RuntimeConfig     json.RawMessage `json:"runtimeConfig"`
	FavoriteFolderIDs []string        `json:"favoriteFolderIds"`
	Saves             []Save          `json:"saves"`
}
type GameInput struct {
	Version            int64           `json:"version"`
	PlatformInstanceID string          `json:"platformInstanceId"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	Developer          string          `json:"developer"`
	Publisher          string          `json:"publisher"`
	Genre              string          `json:"genre"`
	Players            *string         `json:"players"`
	ReleaseYear        *int64          `json:"releaseYear"`
	TagIDs             []string        `json:"tagIds"`
	RuntimeConfig      json.RawMessage `json:"runtimeConfig"`
}
type PreparedGame struct {
	ID          string
	Input       GameInput
	ContentHash string
	Files       []GameFile
	Media       []Media
}

type (
	Page[T any] struct {
		Items  []T   `json:"items"`
		Total  int64 `json:"total"`
		Offset int   `json:"offset"`
		Limit  int   `json:"limit"`
	}
	List[T any] struct {
		Items []T `json:"items"`
	}
	Query struct {
		Offset       int
		Limit        int
		Search       string
		DirectoryID  string
		PlatformID   string
		TagID        string
		FolderID     string
		Unclassified bool
		Favorite     bool
		Status       string
		Kind         string
		Sort         string
		AfterMs      *int64
		BeforeMs     *int64
	}
)
