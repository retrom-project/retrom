package netplayprofile

import "errors"

const (
	ProtocolVersion        = "retrom-netplay-v2"
	ControlCount           = 24
	CheckpointEveryFrames  = 120
	MaxPredictionFrames    = 8
	MaxRollbackFrames      = 120
	CanonicalHistoryFrames = 600
	MaxStateBytes          = 1_048_576
)

var ErrManifestInvalid = errors.New("NETPLAY_MANIFEST_INVALID")

type Protocol struct {
	Version                string   `json:"version"`
	ControlCount           int      `json:"controlCount"`
	CheckpointEveryFrames  int      `json:"checkpointEveryFrames"`
	MaxPredictionFrames    int      `json:"maxPredictionFrames"`
	MaxRollbackFrames      int      `json:"maxRollbackFrames"`
	CanonicalHistoryFrames int      `json:"canonicalHistoryFrames"`
	MaxStateBytes          int      `json:"maxStateBytes"`
	AllowedContentKinds    []string `json:"allowedContentKinds"`
}

type ManifestProfile struct {
	ID                  string   `json:"id"`
	ProviderID          string   `json:"providerId"`
	TargetID            string   `json:"targetId"`
	CoreID              string   `json:"coreId"`
	PlatformIDs         []string `json:"platformIds"`
	MaxPlayers          int      `json:"maxPlayers"`
	MaxPredictionFrames int      `json:"maxPredictionFrames"`
}

type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Protocol      Protocol          `json:"protocol"`
	Profiles      []ManifestProfile `json:"profiles"`
}

type Registry struct {
	Manifest       Manifest
	ManifestDigest string
	profiles       map[string]ManifestProfile
}
