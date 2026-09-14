package gamecontent

import model "retrom/internal/model/gamecontent"

type (
	AdminGameAsset              = model.AdminGameAsset
	AdminGameDetail             = model.AdminGameDetail
	AdminGameFile               = model.AdminGameFile
	AdminGameMetadata           = model.AdminGameMetadata
	AdminGamePatchRequest       = model.AdminGamePatchRequest
	AdminGamePatchResult        = model.AdminGamePatchResult
	AdminGamePatchState         = model.AdminGamePatchState
	AdminGamePatchUpdate        = model.AdminGamePatchUpdate
	AdminGameVariant            = model.AdminGameVariant
	AuditActor                  = model.AuditActor
	JobSnapshot                 = model.JobSnapshot
	PreparedReplacement         = model.PreparedReplacement
	PreparedRPGMakerReplacement = model.PreparedRPGMakerReplacement
	PreparedRPGMakerVariantFile = model.PreparedRPGMakerVariantFile
	ReplacementFile             = model.ReplacementFile
	UploadedFile                = model.UploadedFile
	Repository                  = model.Repository
	ReadScope                   = model.ReadScope
	WriteScope                  = model.WriteScope
	AdminGameReader             = model.AdminGameReader
	AdminGameWriter             = model.AdminGameWriter
	Reader                      = model.Reader
	InputReader                 = model.InputReader
	StoredInput                 = model.StoredInput
	Upload                      = model.Upload
	IdentityFile                = model.IdentityFile
	Binding                     = model.Binding
	Replay                      = model.Replay
	ReplayWrite                 = model.ReplayWrite
	ReplayRecords               = model.ReplayRecords
	ScheduleWrite               = model.ScheduleWrite
	Claim                       = model.Claim
	Outcome                     = model.Outcome
	JobWriter                   = model.JobWriter
	LeaseRecords                = model.LeaseRecords
	Publication                 = model.Publication
	ContentWriter               = model.ContentWriter
	RetirementImpact            = model.RetirementImpact
	ReleaseSignal               = model.ReleaseSignal
	RetirementOwnerKind         = model.RetirementOwnerKind
	RetirementReferenceKind     = model.RetirementReferenceKind
	RetirementOwner             = model.RetirementOwner
	RetirementChange            = model.RetirementChange
	RetirementReference         = model.RetirementReference
	RetirementReader            = model.RetirementReader
	RetirementWriter            = model.RetirementWriter
	RetirementScope             = model.RetirementScope
	DeleteGameReader            = model.DeleteGameReader
	DeleteGameWriter            = model.DeleteGameWriter
	DeleteGameState             = model.DeleteGameState
	DeleteGameImpact            = model.DeleteGameImpact
	DeleteGameReplay            = model.DeleteGameReplay
	DeleteGameReplayWrite       = model.DeleteGameReplayWrite
	DeleteGameAudit             = model.DeleteGameAudit
	DeleteGameRequest           = model.DeleteGameRequest
	DeleteGameResponse          = model.DeleteGameResponse
	DeleteGameResult            = model.DeleteGameResult
)

const (
	DeleteGameOperation         = model.DeleteGameOperation
	RetirementLaunch            = model.RetirementLaunch
	RetirementPlay              = model.RetirementPlay
	RetirementNetplay           = model.RetirementNetplay
	RetirementRoom              = model.RetirementRoom
	RetirementVariant           = model.RetirementVariant
	RetirementSave              = model.RetirementSave
	RetirementLaunchContent     = model.RetirementLaunchContent
	RetirementLaunchExternal    = model.RetirementLaunchExternal
	RetirementVariantFile       = model.RetirementVariantFile
	RetirementVariantDependency = model.RetirementVariantDependency
)

var (
	ErrInvalid                        = model.ErrInvalid
	ErrIdempotencyKeyReused           = model.ErrIdempotencyKeyReused
	ErrExecutionLost                  = model.ErrExecutionLost
	ErrAdminGameNotFound              = model.ErrAdminGameNotFound
	ErrAdminGameVersionConflict       = model.ErrAdminGameVersionConflict
	ErrDeleteGameNotFound             = model.ErrDeleteGameNotFound
	ErrDeleteGameVersionConflict      = model.ErrDeleteGameVersionConflict
	ErrDeleteGameConfirmationMismatch = model.ErrDeleteGameConfirmationMismatch
	ErrDeleteGameImpactStale          = model.ErrDeleteGameImpactStale
)
