package emulationstationimport

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
	AfterID             string
	Limit               int
}

type GamelistQuery struct {
	ImportID, ParseState, AfterPath string
	Limit                           int
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
	Gamelists(context.Context, GamelistQuery) ([]Gamelist, error)
	Collections(context.Context, CollectionQuery) ([]CollectionRecord, error)
	Items(context.Context, ItemQuery) ([]Item, error)
}

type CollectionTags interface {
	EmulationStationReferences(context.Context, []string) (map[string][]tagging.Reference, error)
}
