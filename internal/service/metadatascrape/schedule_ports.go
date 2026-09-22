package metadatascrape

import (
	"context"
)

type (
	Subject       struct{ Kind, ID string }
	ImportSubject struct{ PlatformID string }
	ReviewSubject struct {
		Version      int64
		MetadataJSON string
	}
)

type GameSubject struct {
	Version                    int64
	PlatformID, ManifestDigest string
}
type ScheduleReader interface {
	Import(context.Context, string) (ImportSubject, error)
	Review(context.Context, string) (ReviewSubject, bool, error)
	Game(context.Context, string) (GameSubject, bool, error)
}
type (
	Hashes       struct{ CRC32, MD5, SHA1, SHA256 *string }
	FileEvidence struct {
		Name, BlobID string
		Hashes
		ArchiveBlobID  *string
		ArchiveOrdinal *int64
	}
)

type (
	DATBinding     struct{ ID, SnapshotJSON string }
	ArcadeEvidence struct {
		ArchiveBlobID, Name string
		Ordinal, Size       int64
		CRC32, SHA1         *string
	}
)

type ScheduleEvidenceReader interface {
	Files(context.Context, Subject) ([]FileEvidence, error)
	DAT(context.Context, Subject) (DATBinding, bool, error)
	Arcade(context.Context, Subject, string, string) ([]ArcadeEvidence, error)
}
type HashEvidence struct {
	ID, RunID, Profile    string
	BlobID, ArchiveBlobID *string
	ArchiveOrdinal        *int64
	Hashes
	Order int
	Now   int64
}
type SchedulePlan struct {
	Subject                                                                    Subject
	RunID, JobID, Provider, Dedupe, PayloadJSON, JobState, RunState, EventJSON string
	FinishedAt                                                                 *int64
	Now                                                                        int64
}
type ReviewChange struct {
	ItemID       string
	Version, Now int64
}
type ScheduleWriter interface {
	Create(context.Context, SchedulePlan) error
	Evidence(context.Context, []HashEvidence) error
	Review(context.Context, ReviewChange) error
	Game(context.Context, string, int64, int64) error
}
type ScheduleScope struct {
	Subjects ScheduleReader
	Sources  ScheduleEvidenceReader
	Writes   ScheduleWriter
}
type ScheduleRepository interface {
	WithWrite(context.Context, func(ScheduleScope) error) error
}
type ScrapeDispatcher interface {
	Dispatch(context.Context, string) bool
}
