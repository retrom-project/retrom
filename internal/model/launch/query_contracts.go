package launch

import (
	"context"
	"errors"
)

var ErrCredential = errors.New("LAUNCH_CREDENTIAL_INVALID")

type ContentView struct {
	Digest, Format, CoreID, ProviderID, TargetID, BundleSHA256 string
	DOSEntry                                                   *string
	PlatformKey                                                string
	DiscCount                                                  int
}

type ExternalView struct {
	Digest, Kind, PlatformKey, CoreKey, ProviderID, TargetID, BundleSHA256 string
	DiscCount                                                              int
}

type BundleFile struct{ LogicalName, SHA256 string }

type MultiDiscTelemetryDimensions struct {
	PlatformKey, TargetKey, BundleDigest string
	DiscCount                            int
}

type ProviderAsset struct{ ProviderID, BundleSHA256, Path string }

type SessionRef struct {
	ID      string
	Preview bool
}

type SessionRecord struct {
	CredentialHash                                 []byte
	State                                          string
	HardExpiresAtMS                                int64
	ProviderID, TargetID, BundleSHA256, SaveAccess string
}

type ContentRecord struct {
	Session SessionRecord
	Content ContentView
}

type ExternalRecord struct {
	Session SessionRecord
	Content ExternalView
}

type BundleRecord struct {
	Session SessionRecord
	Files   []BundleFile
}

type MultiDiscRecord struct {
	Session    SessionRecord
	Dimensions MultiDiscTelemetryDimensions
}

type ContentReader interface {
	ProductContent(context.Context, string, string, bool) (ContentRecord, bool, error)
	PreviewContent(context.Context, string, string) (ContentRecord, bool, error)
	PreviewProject(context.Context, string, string, bool) (ContentRecord, bool, error)
	External(context.Context, SessionRef, string) (ExternalRecord, bool, error)
}

type SessionReader interface {
	Session(context.Context, SessionRef) (SessionRecord, bool, error)
	SaveSession(context.Context, string) (SessionRecord, bool, error)
	Bundle(context.Context, SessionRef, string) (BundleRecord, bool, error)
	MultiDisc(context.Context, string) (MultiDiscRecord, bool, error)
}

type TargetAssets interface {
	AssetPaths(string, string) ([]string, bool)
}

type MatchCapability func(string, []byte) bool
