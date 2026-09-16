package tagging

// CreateCommand carries all values needed for a single tag creation.
// The service prepares IDs and timestamps; the repo reads facts and
// applies pure policies inside its own short transaction.
type CreateCommand struct {
	TagID, AuditID, ActorUserID string
	Name, NameKey, SearchText   string
	NowMS                       int64
}

// RenameCommand carries values for renaming one tag at an expected version.
type RenameCommand struct {
	TagID, AuditID, ActorUserID string
	Name, NameKey, SearchText   string
	ExpectedVersion, NowMS      int64
}

// DeleteCommand carries values for soft-deleting one tag at an expected version.
type DeleteCommand struct {
	TagID, AuditID, ActorUserID string
	ConfirmName                 string
	ExpectedVersion, NowMS      int64
}

// ReplaceGameTagsCommand carries values for replacing game-tag associations.
type ReplaceGameTagsCommand struct {
	GameID, AuditID, ActorUserID string
	ExpectedVersion, NowMS       int64
	TagIDs                       []string
}

// EnsureCommonTagsCommand carries prepared candidates for the common taxonomy.
type EnsureCommonTagsCommand struct {
	ActorUserID string
	NowMS       int64
	Candidates  []CommonTagCandidate
}

// CommonTagCandidate holds one pre-normalized common tag with a prepared ID.
type CommonTagCandidate struct {
	TagID, AuditID            string
	Name, NameKey, SearchText string
}
