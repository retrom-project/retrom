package gamevariant

import (
	"context"
	"errors"

	contentcapability "retrom/internal/content/capability"
	corevalidation "retrom/internal/core/validation"
	validation "retrom/internal/service/corevalidation"
)

var ErrBlocked = errors.New("VARIANT_VALIDATION_FAILED")

type Source struct {
	ActiveDATVersionID                                              *string
	ValidationLogicalName                                           string
	GameID, InstanceID, PlatformID, CoreID, BindingID               string
	ProviderID, TargetID, BundleSHA256, DeliveryProfile             string
	ContentKind, ContentLogicalName, SourceManifestDigest           string
	VariantID, VariantStatus, DependencySnapshot, CompatibilityCode string
	GameVersion                                                     int64
	DATVersionID                                                    *string
	ContentPolicy                                                   contentcapability.Policy
	ReadFormats                                                     []string
}
type File struct {
	Role, FileRecord, LogicalName, Digest string
	SortOrder                             int
	SizeBytes                             int64
}
type ArcadeBIOS struct {
	State          string
	Dependency     corevalidation.BIOSDependency
	OptionsJSON    *string
	CatalogPresent bool
}
type BIOSFacts struct {
	Static []validation.BIOSRecord
	Arcade []ArcadeBIOS
}
type VariantWrite struct {
	Source Source
	NowMS  int64
}
type WriteScope interface {
	ValidationJobRepository
	CreateVariant(context.Context, VariantWrite) error
	MarkPending(context.Context, string, int64) error
}

type Snapshot struct {
	Found                   bool
	Source                  Source
	GameFiles, VariantFiles []File
	BIOS, ValidationBIOS    BIOSFacts
}
