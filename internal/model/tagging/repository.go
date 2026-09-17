package tagging

import "context"

type Repository interface {
	Get(context.Context, string) (AdminItem, error)
	List(context.Context, ListQuery) ([]AdminItem, error)
	Summary(context.Context) (Summary, error)
	References(context.Context, OwnerKind, []string) (map[string][]Reference, error)
}

// ReferenceReader is the read-only port used by application projections that
// need the relations owned by a tagging aggregate. It belongs to the model
// boundary so model packages do not have to depend on the service package.
type ReferenceReader interface {
	References(context.Context, Owner) ([]Reference, error)
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
