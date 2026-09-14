package tagging

import "context"

type Repository interface {
	WithWrite(context.Context, func(WriteScope) error) error
	Get(context.Context, string) (AdminItem, error)
	List(context.Context, ListQuery) ([]AdminItem, error)
	Summary(context.Context) (Summary, error)
	References(context.Context, OwnerKind, []string) (map[string][]Reference, error)
}

type WriteScope struct {
	Tags      TagReader
	Changes   TagWriter
	Relations RelationRecords
	Games     GameRecords
	Audit     AuditRecords
}

type TagReader interface {
	Get(context.Context, string) (AdminItem, error)
	ActiveByNameKey(context.Context) (map[string]string, error)
	ActiveReferences(context.Context, []string) ([]Reference, error)
}

type TagWriter interface {
	Insert(context.Context, TagWrite) error
	Rename(context.Context, TagWrite) error
	Delete(context.Context, TagWrite) error
}

type RelationRecords interface {
	References(context.Context, Owner) ([]Reference, error)
	Add(context.Context, Assignment) error
	Remove(context.Context, Owner, []string) error
	TouchTags(context.Context, string, []string, int64) error
}

type GameRecords interface {
	Version(context.Context, string) (int64, error)
	Touch(context.Context, string, int64, int64) error
}

type AuditRecords interface {
	Record(context.Context, AuditEvent) error
}

type OwnerKind string

const (
	OwnerGame                       OwnerKind = "GAME"
	OwnerReviewDraft                OwnerKind = "REVIEW_DRAFT"
	OwnerReviewItem                 OwnerKind = "REVIEW_ITEM"
	OwnerPegasusCollection          OwnerKind = "PEGASUS_COLLECTION"
	OwnerEmulationStationCollection OwnerKind = "EMULATIONSTATION_COLLECTION"
)

type Owner struct {
	Kind OwnerKind
	ID   string
}
type Assignment struct {
	Owner       Owner
	References  []Reference
	ActorUserID string
	NowMS       int64
}

type TagWrite struct {
	ID, Name, NameKey, SearchText, ActorUserID string
	ExpectedVersion, NowMS                     int64
}

type ListQuery struct {
	Status, Sort, SearchText, AfterID, AfterNameKey string
	AfterUpdatedAt                                  int64
	Limit                                           int
}

type AuditEvent struct {
	ID, ActorUserID, Action, ResourceType, ResourceID string
	Before, After, Diff                               []byte
	CreatedAtMS                                       int64
}
