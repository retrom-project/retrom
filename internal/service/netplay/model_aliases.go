package netplay

import model "retrom/internal/model/netplay"

type (
	PeerIdentity                = model.PeerIdentity
	ParticipantAccessRepository = model.ParticipantAccessRepository
	ParticipantCredentialRecord = model.ParticipantCredentialRecord
	ParticipantLaunchResult     = model.ParticipantLaunchResult
	PreparationAborter          = model.PreparationAborter
	PreparationPlan             = model.PreparationPlan
	PreparationReader           = model.PreparationReader
	PreparationRepository       = model.PreparationRepository
	PreparationRequest          = model.PreparationRequest
	PreparationScope            = model.PreparationScope
	PreparationSnapshot         = model.PreparationSnapshot
	PreparationWriter           = model.PreparationWriter
	CredentialSigner            = model.CredentialSigner
	NetplayLauncher             = model.NetplayLauncher
	SocketAccessRecord          = model.SocketAccessRecord
	SocketParticipant           = model.SocketParticipant
	RoomClearPlan               = model.RoomClearPlan
	RoomControlEvidence         = model.RoomControlEvidence
	RoomControlReader           = model.RoomControlReader
	MutationCommand             = model.MutationCommand
	RoomControlRepository       = model.RoomControlRepository
	RoomControlScope            = model.RoomControlScope
	RoomControlSnapshot         = model.RoomControlSnapshot
	RoomControlWriter           = model.RoomControlWriter
	RoomCreationPlan            = model.RoomCreationPlan
	RoomCreationRepository      = model.RoomCreationRepository
	RoomCreationWriter          = model.RoomCreationWriter
	RoomCapacity                = model.RoomCapacity
	RoomEventPage               = model.RoomEventPage
	RoomEventsRepository        = model.RoomEventsRepository
	Event                       = model.Event
	RoomFilter                  = model.RoomFilter
	RoomQueryRepository         = model.RoomQueryRepository
	ExpiryCandidate             = model.ExpiryCandidate
	ExpiryCutoffs               = model.ExpiryCutoffs
	ExpiryPlan                  = model.ExpiryPlan
	ExpiredSessionEnder         = model.ExpiredSessionEnder
	MaintenanceRepository       = model.MaintenanceRepository
	MaintenanceWriter           = model.MaintenanceWriter
	RecoveryPlan                = model.RecoveryPlan
	RoomReadyPlan               = model.RoomReadyPlan
	RoomSeatPlan                = model.RoomSeatPlan
	RoomSelection               = model.RoomSelection
	RoomSelectionPlan           = model.RoomSelectionPlan
	SeatMember                  = model.SeatMember
	ArcadeDependencyRow         = model.ArcadeDependencyRow
	EligibleProfile             = model.EligibleProfile
	EligibilityRepository       = model.EligibilityRepository
	EligibilityRow              = model.EligibilityRow
	GameSummary                 = model.GameSummary
	ProfileSummary              = model.ProfileSummary
	BIOSResolver                = model.BIOSResolver
	TagReader                   = model.TagReader
	PeerTransitionMode          = model.PeerTransitionMode
	PeerTransitionPlan          = model.PeerTransitionPlan
	ResyncCause                 = model.ResyncCause
	Room                        = model.Room
	RoomEndPlan                 = model.RoomEndPlan
	RoomExitReader              = model.RoomExitReader
	RoomExitRepository          = model.RoomExitRepository
	RoomExitScope               = model.RoomExitScope
	RoomExitSnapshot            = model.RoomExitSnapshot
	RoomExitWriter              = model.RoomExitWriter
	RoomGame                    = model.RoomGame
	RoomMember                  = model.RoomMember
	RoomPermissions             = model.RoomPermissions
	RoomRemovalPlan             = model.RoomRemovalPlan
	FrozenRoomProfile           = model.FrozenRoomProfile
	SessionStartPlan            = model.SessionStartPlan
	SessionStartRepository      = model.SessionStartRepository
	SessionStartScope           = model.SessionStartScope
	SessionStartWriter          = model.SessionStartWriter
	SessionControlReader        = model.SessionControlReader
	SessionControlRepository    = model.SessionControlRepository
	SessionControlScope         = model.SessionControlScope
	SessionControlSnapshot      = model.SessionControlSnapshot
	SessionControlWriter        = model.SessionControlWriter
	SessionEvent                = model.SessionEvent
	SessionEventData            = model.SessionEventData
	SessionPeer                 = model.SessionPeer
	SessionSummary              = model.SessionSummary
	SessionTransitionPlan       = model.SessionTransitionPlan
)

const (
	PeersConnect       = model.PeersConnect
	PeersPrepareResync = model.PeersPrepareResync
	PeersUnchanged     = model.PeersUnchanged
	ResyncHash         = model.ResyncHash
	ResyncHost         = model.ResyncHost
	ResyncReconnect    = model.ResyncReconnect
	RoomStateDraft     = model.RoomStateDraft
	RoomStateRunning   = model.RoomStateRunning
	RoomStateStarting  = model.RoomStateStarting
	RoomStateWaiting   = model.RoomStateWaiting
)

var (
	ErrCapacity              = model.ErrCapacity
	ErrInvalidRecoveryReason = model.ErrInvalidRecoveryReason
	ErrInvalidProfile        = model.ErrInvalidProfile
	ErrForbidden             = model.ErrForbidden
	ErrInvalidSeat           = model.ErrInvalidSeat
	ErrPrecondition          = model.ErrPrecondition
	ErrProfileStale          = model.ErrProfileStale
	ErrRoomConflict          = model.ErrRoomConflict
	ErrRoomNotFound          = model.ErrRoomNotFound
	ErrRoomNotReady          = model.ErrRoomNotReady
	ErrSeatTaken             = model.ErrSeatTaken
	ErrSessionNotFound       = model.ErrSessionNotFound
)
