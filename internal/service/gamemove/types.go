package gamemove

import (
	"errors"
)

var (
	ErrInvalid               = errors.New("GAME_MOVE_INVALID")
	ErrImpactStale           = errors.New("GAME_MOVE_IMPACT_STALE")
	ErrVersionConflict       = errors.New("GAME_MOVE_VERSION_CONFLICT")
	ErrValidationUnavailable = errors.New("GAME_MOVE_VALIDATION_UNAVAILABLE")
)

// Impact is the auditable move decision returned to the HTTP adapter.
type Impact struct {
	Action                   string   `json:"action"`
	GameID                   string   `json:"gameId"`
	GameVersion              int64    `json:"gameVersion"`
	SourcePlatformInstanceID string   `json:"sourcePlatformInstanceId"`
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	TargetPlatformVersion    int64    `json:"targetPlatformInstanceVersion"`
	TargetCoreID             string   `json:"targetCoreId"`
	TargetProviderID         string   `json:"targetProviderId"`
	TargetID                 string   `json:"targetId"`
	TargetDATVersionID       *string  `json:"targetDatVersionId"`
	ValidationInputDigest    string   `json:"validationInputDigest"`
	VariantStatus            string   `json:"variantStatus"`
	BlockerCodes             []string `json:"blockerCodes"`
}

type PreviewRequest struct {
	GameID                   string
	TargetPlatformInstanceID string
	ExpectedVersion          int64
}

// ImpactSubject is the stable game/target binding snapshot used to build an
// impact. It is returned by the repository before validation is resolved.
type ImpactSubject struct {
	GameID                   string
	SourcePlatformInstanceID string
	SourcePlatformID         string
	ContentLogicalName       string
	GameVersion              int64
	TargetPlatformInstanceID string
	TargetPlatformID         string
	TargetCoreID             string
	TargetPlatformVersion    int64
	TargetProviderID         string
	TargetID                 string
	TargetDATVersionID       *string
}

type VariantQuery struct {
	GameID, CoreID, ProviderID, TargetID string
	DATVersionID                         *string
}

type VariantState struct {
	Status, CompatibilityCode string
}

type MoveRequest struct {
	GameID                   string
	TargetPlatformInstanceID string
	ExpectedVersion          int64
	NowMS                    int64
	Impact                   Impact
	Actor                    AuditActor
}

type MoveResult struct {
	GameID             string
	PlatformInstanceID string
	Version            int64
	UpdatedAtMS        int64
}

type AuditActor struct {
	Kind      string
	UserID    any
	Label     any
	RequestID string
}

type AuditEvent struct {
	ID, Action, ResourceType, ResourceID string
	Before, After                        any
	Actor                                AuditActor
	CreatedAtMS                          int64
}

type CandidateRecord struct {
	ID, ProviderGameID, MetadataJSON, EvidenceJSON string
	CreatedAtMS, HitCount                          int64
}

type ScrapeCandidatesResult struct {
	RunID *string
	Items []CandidateRecord
}
