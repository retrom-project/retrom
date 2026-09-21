package tagging

import "errors"

var (
	ErrInvalid                 = errors.New("TAG_INVALID")
	ErrNameInvalid             = errors.New("TAG_NAME_INVALID")
	ErrNotFound                = errors.New("TAG_NOT_FOUND")
	ErrGameNotFound            = errors.New("GAME_NOT_FOUND")
	ErrNameConflict            = errors.New("TAG_NAME_CONFLICT")
	ErrLimitReached            = errors.New("TAG_LIMIT_REACHED")
	ErrAlreadyDeleted          = errors.New("TAG_ALREADY_DELETED")
	ErrVersionConflict         = errors.New("VERSION_CONFLICT")
	ErrReferenceInvalid        = errors.New("TAG_REFERENCE_INVALID")
	ErrAssignmentLimitExceeded = errors.New("TAG_ASSIGNMENT_LIMIT_EXCEEDED")
	ErrDeleteConfirmation      = errors.New("TAG_DELETE_CONFIRMATION_MISMATCH")
)

const (
	StatusActive  = "ACTIVE"
	StatusDeleted = "DELETED"

	SortNameAsc     = "NAME_ASC"
	SortUpdatedDesc = "UPDATED_DESC"

	MaxActiveTags    = 1000
	MaxTagsPerOwner  = 20
	DefaultListLimit = 50
	MaximumListLimit = 100
	MaximumNameRunes = 40
	MaximumNameBytes = 160
)

type Reference struct {
	TagID string `json:"tagId"`
	Name  string `json:"name"`
}

type Usage struct {
	PublishedGameCount              int64 `json:"publishedGameCount"`
	DeletedGameCount                int64 `json:"deletedGameCount"`
	ReviewDraftCount                int64 `json:"reviewDraftCount"`
	PegasusCollectionCount          int64 `json:"pegasusCollectionCount"`
	EmulationStationCollectionCount int64 `json:"emulationStationCollectionCount"`
}

type AdminItem struct {
	TagID       string `json:"tagId"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	Usage       Usage  `json:"usage"`
	CreatedAtMS int64  `json:"createdAtMs"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
	DeletedAtMS *int64 `json:"deletedAtMs"`
}

type Summary struct {
	ActiveTagCount     int64 `json:"activeTagCount"`
	TaggedGameCount    int64 `json:"taggedGameCount"`
	PendingReviewCount int64 `json:"pendingReviewCount"`
}

type CommonTagsResult struct {
	CreatedItems  []AdminItem `json:"createdItems"`
	ExistingItems []AdminItem `json:"existingItems"`
}

type ListFilter struct {
	Query       string
	Status      string
	Sort        string
	AfterValues []string
	AfterID     string
	Limit       int
}

type DeleteImpact struct {
	PublishedGameCount              int64 `json:"publishedGameCount"`
	DeletedGameCount                int64 `json:"deletedGameCount"`
	ReviewDraftCount                int64 `json:"reviewDraftCount"`
	PegasusCollectionCount          int64 `json:"pegasusCollectionCount"`
	EmulationStationCollectionCount int64 `json:"emulationStationCollectionCount"`
}

type GameTagResult struct {
	GameID  string      `json:"gameId"`
	Version int64       `json:"version"`
	Tags    []Reference `json:"tags"`
}

type InvalidReferencesError struct {
	IDs []string
}

func (err *InvalidReferencesError) Error() string { return ErrReferenceInvalid.Error() }
func (err *InvalidReferencesError) Unwrap() error { return ErrReferenceInvalid }
