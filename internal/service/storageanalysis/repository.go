package storageanalysis

import "context"

type Repository interface {
	Read(context.Context) (ReadModel, error)
}

// ReadModel contains one consistent snapshot. Reference ID lists are distinct.
type ReadModel struct {
	Blobs             map[string]int64
	Protected         map[string]struct{}
	Usage             map[string]Usage
	Archives          []ArchiveMember
	Saves             SaveReferences
	CleanupCandidates []string
}

type (
	ArchiveMember  struct{ ArchiveID, MemberID string }
	SaveReferences struct {
		ActiveCount, DeletedCount int64
		PayloadIDs, ScreenshotIDs []string
	}
)
