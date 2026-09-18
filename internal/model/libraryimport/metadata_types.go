package libraryimport

import "context"

type ServerMetadata struct {
	Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                            *int
}

type ServerMetadataWarning struct {
	Code  string `json:"code"`
	Field string `json:"field"`
}

type MetadataDraft struct {
	MetadataJSON string
	Version      int64
}

type MetadataAudit struct {
	ID, ActorKind           string
	ActorUserID, ActorLabel *string
	BeforeJSON, AfterJSON   string
}

type MetadataChange struct {
	ItemID                   string
	Before                   MetadataDraft
	MetadataJSON, SearchText string
	Audit                    MetadataAudit
	NowMS                    int64
}

type MetadataScope interface {
	CurrentMetadata(context.Context, string) (MetadataDraft, error)
	SaveMetadata(context.Context, MetadataChange) error
}

type MetadataRepository interface {
	LoadCurrentMetadata(context.Context, string) (MetadataDraft, error)
	CommitMetadataChange(context.Context, MetadataChange) error
}
