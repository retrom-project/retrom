package libraryimport

import (
	"context"

	contentcapability "retrom/internal/content/capability"
)

type ReviewProfileReader interface {
	Profile(context.Context, string) (RPGReviewProfile, bool, error)
}
type ReviewRuntimeInputs struct {
	DraftID, EffectiveSnapshotID, EffectiveManifestDigest, ContentKind string
	PlatformVersion                                                    int64
	CoreID, ProviderID, RuntimeTargetID                                string
	DATVersionID                                                       *string
	ContentPolicy                                                      contentcapability.Policy
}
