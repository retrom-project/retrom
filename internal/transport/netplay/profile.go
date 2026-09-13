package netplay

import (
	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/transport/netplay/profile"
)

const (
	ProtocolVersion        = profile.ProtocolVersion
	WebSocketSubprotocol   = profile.WebSocketSubprotocol
	ControlCount           = profile.ControlCount
	CheckpointEveryFrames  = profile.CheckpointEveryFrames
	MaxPredictionFrames    = profile.MaxPredictionFrames
	MaxRollbackFrames      = profile.MaxRollbackFrames
	CanonicalHistoryFrames = profile.CanonicalHistoryFrames
	MaxStateBytes          = profile.MaxStateBytes
	ManifestRelativePath   = profile.ManifestRelativePath
	ManifestSchemaRelative = profile.ManifestSchemaRelative
)

type (
	Protocol              = profile.Protocol
	ManifestProfile       = profile.ManifestProfile
	Manifest              = profile.Manifest
	Registry              = profile.Registry
	CanonicalProfileInput = profile.CanonicalProfileInput
)

var ErrManifestInvalid = profile.ErrManifestInvalid

func LoadRegistry(root string, set *dependencies.Set) (*Registry, error) {
	registry, err := profile.LoadRegistry(root, set)
	if err != nil {
		return nil, serviceError("load registry", err)
	}
	return registry, nil
}
func validDigest(value string) bool { return profile.ValidDigest(value) }
