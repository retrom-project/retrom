package saves

import model "retrom/internal/model/saves"

type (
	BlobRecords             = model.BlobRecords
	CheckpointRecords       = model.CheckpointRecords
	DeleteRequest           = model.DeleteRequest
	Duration                = model.Duration
	GameSaveBinding         = model.GameSaveBinding
	GameSaveRecords         = model.GameSaveRecords
	IdempotencyRecords      = model.IdempotencyRecords
	Launch                  = model.Launch
	LaunchReader            = model.LaunchReader
	ListItem                = model.ListItem
	ListQuery               = model.ListQuery
	ListRepository          = model.ListRepository
	ManualResult            = model.ManualResult
	PreviewWrite            = model.PreviewWrite
	RenameRequest           = model.RenameRequest
	Replay                  = model.Replay
	ReplayKey               = model.ReplayKey
	ReplayWrite             = model.ReplayWrite
	Repository              = model.Repository
	Restore                 = model.Restore
	SaveCreation            = model.SaveCreation
	SaveUpdate              = model.SaveUpdate
	StateMutationRepository = model.StateMutationRepository
	StoredSave              = model.StoredSave
	WriteScope              = model.WriteScope
)

var (
	ErrNotFound               = model.ErrNotFound
	ErrRepositoryUnavailable  = model.ErrRepositoryUnavailable
	ErrVersionConflict        = model.ErrVersionConflict
	ErrCheckpointIncompatible = model.ErrCheckpointIncompatible
	ErrCheckpointInvalid      = model.ErrCheckpointInvalid
	ErrCheckpointUnavailable  = model.ErrCheckpointUnavailable
	ErrCredential             = model.ErrCredential
	ErrInvalid                = model.ErrInvalid
	ErrSequenceReused         = model.ErrSequenceReused
	ErrSyncConflict           = model.ErrSyncConflict
	ErrTooLarge               = model.ErrTooLarge
)
