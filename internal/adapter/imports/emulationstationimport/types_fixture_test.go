package emulationstationimport

import (
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

type (
	CreateRequest      = emulationstationimportmodel.CreateRequest
	RootRef            = emulationstationimportmodel.RootRef
	CreatedBy          = emulationstationimportmodel.CreatedBy
	Counts             = emulationstationimportmodel.Counts
	Summary            = emulationstationimportmodel.Summary
	Collection         = emulationstationimportmodel.Collection
	ExtensionSummary   = emulationstationimportmodel.ExtensionSummary
	Gamelist           = emulationstationimportmodel.Gamelist
	Mapping            = emulationstationimportmodel.Mapping
	Item               = emulationstationimportmodel.Item
	SourceFlags        = emulationstationimportmodel.SourceFlags
	FailureDetails     = emulationstationimportmodel.FailureDetails
	RuntimeCheck       = emulationstationimportmodel.RuntimeCheck
	RuntimeDependency  = emulationstationimportmodel.RuntimeDependency
	RuntimeBIOS        = emulationstationimportmodel.RuntimeBIOS
	RuntimeMissingDisc = emulationstationimportmodel.RuntimeMissingDisc
	ItemMedia          = emulationstationimportmodel.ItemMedia
	ExistingMatch      = emulationstationimportmodel.ExistingMatch
)

type (
	executionItem  = emulationstationimportmodel.ExecutionItem
	executionFile  = emulationstationimportmodel.ExecutionFile
	executionAsset = emulationstationimportmodel.ExecutionAsset
)

var errImportCancelled = emulationstationimportservice.ErrExecutionCancelled
