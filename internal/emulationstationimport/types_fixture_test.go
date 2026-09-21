package emulationstationimport

import (
	application "retrom/internal/service/emulationstationimport"
)

type (
	CreateRequest      = application.CreateRequest
	RootRef            = application.RootRef
	CreatedBy          = application.CreatedBy
	Counts             = application.Counts
	Summary            = application.Summary
	Collection         = application.Collection
	ExtensionSummary   = application.ExtensionSummary
	Gamelist           = application.Gamelist
	Mapping            = application.Mapping
	Item               = application.Item
	SourceFlags        = application.SourceFlags
	FailureDetails     = application.FailureDetails
	RuntimeCheck       = application.RuntimeCheck
	RuntimeDependency  = application.RuntimeDependency
	RuntimeBIOS        = application.RuntimeBIOS
	RuntimeMissingDisc = application.RuntimeMissingDisc
	ItemMedia          = application.ItemMedia
	ExistingMatch      = application.ExistingMatch
)

type (
	executionItem  = application.ExecutionItem
	executionFile  = application.ExecutionFile
	executionAsset = application.ExecutionAsset
)

var errImportCancelled = application.ErrExecutionCancelled
