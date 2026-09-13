package netplay

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/service/tagging"
	"retrom/internal/transport/netplay/profile"
)

var ErrInvalidProfile = errors.New("NETPLAY_INVALID_PROFILE")

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

type EligibilityRepository interface {
	GamePage(context.Context, string, string, string, int) ([]GameSummary, bool, error)
	Rows(context.Context, string) ([]EligibilityRow, error)
	ArcadeDependencies(context.Context, string, string) ([]ArcadeDependencyRow, error)
	DependencyFileCount(context.Context, string, string, string) (int, error)
}
type TagReader interface {
	References(context.Context, []string) (map[string][]tagging.Reference, error)
}
type BIOSResolver interface {
	ResolveBIOS(context.Context, string, string, string) (corevalidation.Snapshot, string, string, error)
}
type EligibilityRow struct {
	VariantID, ProviderID, TargetID, BundleSHA256, SourceManifestDigest    string
	PlatformID, CoreID, CoreName, DependencyJSON, ContentKind, LogicalName string
	DATVersionID                                                           *string
}
type (
	ArcadeDependencyRow struct{ Kind, LogicalArchive, Machine, RequiredEntriesJSON, State string }
	Eligibility         struct {
		repository EligibilityRepository
		registry   *profile.Registry
		tags       TagReader
		bios       BIOSResolver
	}
)

func NewEligibility(
	repository EligibilityRepository, registry *profile.Registry, tags TagReader, bios BIOSResolver,
) *Eligibility {
	return &Eligibility{repository: repository, registry: registry, tags: tags, bios: bios}
}
