package pegasusimport

import (
	pegasusimportmodel "retrom/internal/model/pegasusimport"
)

var (
	ErrNotFound        = pegasusimportmodel.ErrNotFound
	ErrMetadataAbsent  = pegasusimportmodel.ErrMetadataAbsent
	ErrMapping         = pegasusimportmodel.ErrMapping
	ErrVersionConflict = pegasusimportmodel.ErrVersionConflict
	ErrNoSelection     = pegasusimportmodel.ErrNoSelection
	ErrExpired         = pegasusimportmodel.ErrExpired
	ErrActive          = pegasusimportmodel.ErrActive
	ErrInvalid         = pegasusimportmodel.ErrInvalid
	ErrNotCancellable  = pegasusimportmodel.ErrNotCancellable
	ErrNotRetryable    = pegasusimportmodel.ErrNotRetryable
)

type CreateRequest = pegasusimportmodel.CreateRequest

type (
	RootRef            = pegasusimportmodel.RootRef
	CreatedBy          = pegasusimportmodel.CreatedBy
	Counts             = pegasusimportmodel.Counts
	Summary            = pegasusimportmodel.Summary
	Collection         = pegasusimportmodel.Collection
	Mapping            = pegasusimportmodel.Mapping
	Item               = pegasusimportmodel.Item
	FailureDetails     = pegasusimportmodel.FailureDetails
	RuntimeCheck       = pegasusimportmodel.RuntimeCheck
	RuntimeDependency  = pegasusimportmodel.RuntimeDependency
	RuntimeBIOS        = pegasusimportmodel.RuntimeBIOS
	RuntimeMissingDisc = pegasusimportmodel.RuntimeMissingDisc
	ItemMedia          = pegasusimportmodel.ItemMedia
	ExistingMatch      = pegasusimportmodel.ExistingMatch
)
