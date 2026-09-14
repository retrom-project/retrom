package pegasusimport

import (
	"context"

	"retrom/internal/model/tagging"
)

type ListQuery struct {
	State      string
	BeforeAtMS int64
	BeforeID   string
	Limit      int
}

type CollectionQuery struct {
	ImportID, AfterPath string
	AfterOrdinal        int64
	AfterID             string
	Limit               int
}

type ItemQuery struct {
	ImportID, Text, Outcome, Warning, CollectionID, AfterTitle, AfterID string
	Limit                                                               int
}

type CollectionRecord struct {
	Collection
	ImportState string
}

type QueryRepository interface {
	Get(context.Context, string) (Summary, error)
	List(context.Context, ListQuery) ([]Summary, error)
	Collections(context.Context, CollectionQuery) ([]CollectionRecord, error)
	Items(context.Context, ItemQuery) ([]Item, error)
}

type CollectionTags interface {
	PegasusReferences(context.Context, []string) (map[string][]tagging.Reference, error)
}
