package bios

import model "retrom/internal/model/bios"

type (
	Cursor       = model.Cursor
	Installation = model.Installation
	Item         = model.Item
	ListRequest  = model.ListRequest
	ListResult   = model.ListResult
	Repository   = model.Repository
	ScopeCounts  = model.ScopeCounts
	Summary      = model.Summary
)

const (
	QuickAll               = model.QuickAll
	QuickAttention         = model.QuickAttention
	QuickOptional          = model.QuickOptional
	QuickRequired          = model.QuickRequired
	ScopeFullCatalog       = model.ScopeFullCatalog
	ScopeRequiredByLibrary = model.ScopeRequiredByLibrary
)

var ErrInvalid = model.ErrInvalid
