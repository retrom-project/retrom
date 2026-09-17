package pegasusimport

import application "retrom/internal/model/pegasusimport"

var (
	ErrNotFound        = application.ErrNotFound
	ErrMetadataAbsent  = application.ErrMetadataAbsent
	ErrMapping         = application.ErrMapping
	ErrVersionConflict = application.ErrVersionConflict
	ErrNoSelection     = application.ErrNoSelection
	ErrExpired         = application.ErrExpired
	ErrActive          = application.ErrActive
	ErrInvalid         = application.ErrInvalid
	ErrNotCancellable  = application.ErrNotCancellable
	ErrNotRetryable    = application.ErrNotRetryable
)

type CreateRequest = application.CreateRequest

type (
	RootRef            = application.RootRef
	CreatedBy          = application.CreatedBy
	Counts             = application.Counts
	Summary            = application.Summary
	Collection         = application.Collection
	Mapping            = application.Mapping
	Item               = application.Item
	FailureDetails     = application.FailureDetails
	RuntimeCheck       = application.RuntimeCheck
	RuntimeDependency  = application.RuntimeDependency
	RuntimeBIOS        = application.RuntimeBIOS
	RuntimeMissingDisc = application.RuntimeMissingDisc
	ItemMedia          = application.ItemMedia
	ExistingMatch      = application.ExistingMatch
)
