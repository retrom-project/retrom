package gamelist

import model "retrom/internal/model/gamelist"

type (
	CoreOption    = model.CoreOption
	Cursor        = model.Cursor
	DOSEntry      = model.DOSEntry
	Detail        = model.Detail
	Facet         = model.Facet
	Facets        = model.Facets
	Filters       = model.Filters
	GameItem      = model.GameItem
	ListRequest   = model.ListRequest
	ListResult    = model.ListResult
	NamedResource = model.NamedResource
	Reason        = model.Reason
	Repository    = model.Repository
	SaveState     = model.SaveState
)

const (
	SortAddedDesc   = model.SortAddedDesc
	SortRecentDesc  = model.SortRecentDesc
	SortTitleAsc    = model.SortTitleAsc
	SortUpdatedDesc = model.SortUpdatedDesc
)

var (
	ErrInvalid  = model.ErrInvalid
	ErrNotFound = model.ErrNotFound
)
