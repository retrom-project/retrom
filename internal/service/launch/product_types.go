package launch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
	validation "retrom/internal/service/corevalidation"
)

var (
	ErrIdempotencyKeyReused = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrDOSEntryMissing      = errors.New("LAUNCH_DOS_ENTRY_MISSING")
	ErrDOSEntryUnsafe       = errors.New("LAUNCH_DOS_ENTRY_UNSAFE")
)

type CreateRequest struct {
	GameID             string       `json:"gameId"`
	CoreID             *string      `json:"coreId"`
	SaveStateID        *string      `json:"saveStateId"`
	DOSEntry           *string      `json:"dosEntry"`
	ReturnTo           string       `json:"returnTo"`
	ClientCapabilities Capabilities `json:"clientCapabilities"`
}
type ProductCreateCommand struct {
	ActorID, ProfileID, Key, Digest string
	Request                         CreateRequest
}
type ProductReceipt struct {
	Digest                   string
	Status                   int
	Body                     json.RawMessage
	Created                  Created
	Replayed                 bool
	CreatedAtMS, ExpiresAtMS int64
}
type ProductSource struct {
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
type ProductSave struct {
	ID, ProfileID, GameID, SourceCoreID, PayloadID, Format, Digest string
	SizeBytes                                                      int64
	DOSEntry                                                       *string
	DiscIndex                                                      *int64
}
type ProductFile struct {
	Role, BlobID, LogicalName, Digest string
	SortOrder                         int
	SizeBytes                         int64
}
type ProductArcadeBIOS struct {
	State          string
	Dependency     corevalidation.BIOSDependency
	OptionsJSON    *string
	CatalogPresent bool
}
type ProductBIOSFacts struct {
	Static []validation.BIOSRecord
	Arcade []ProductArcadeBIOS
}
type (
	ProductDOSEntry struct{ Found, Safe bool }
	ProductSnapshot struct {
		Found                   bool
		Source                  ProductSource
		Save                    *ProductSave
		SaveReadable            bool
		GameFiles, VariantFiles []ProductFile
		BIOS                    ProductBIOSFacts
		ValidationBIOS          ProductBIOSFacts
		DOS                     ProductDOSEntry
	}
)

type (
	ProductContentFile  struct{ BlobID, LogicalName, Format string }
	ProductExternalFile struct{ Kind, BlobID, LogicalName, VirtualPath string }
	ProductDisc         struct {
		BlobID, Digest, LogicalName, VirtualPath string
		Index                                    int
		SizeBytes                                int64
	}
)

type ProductContent struct {
	Checks []ProductBlobCheck
	Files  []ProductContentFile
	Discs  []ProductDisc
}
type ProductCreatePlan struct {
	Command                      ProductCreateCommand
	Source                       ProductSource
	Content                      ProductContent
	External                     []ProductExternalFile
	ID                           string
	CredentialHash               []byte
	SelectedDOSEntry             *string
	InitialDiscIndex             int64
	NowMS, BootstrapEnd, HardEnd int64
	Isolation                    *IsolationTicket
	OverrideBIOS                 *corevalidation.Snapshot
}
type ProductBlobCheck struct {
	Digest    string
	SizeBytes int64
	Exact     []byte
}
type ProductBlobVerifier interface {
	Verify(context.Context, ProductBlobCheck) error
}
type ProductVariantWrite struct {
	Source ProductSource
	NowMS  int64
}
type ProductValidationScope interface {
	ValidationJobRepository
	CreateVariant(context.Context, ProductVariantWrite) error
	MarkPending(context.Context, string, int64) error
}
type ProductCreationScope interface {
	Replay(context.Context, ProductCreateCommand) (ProductReceipt, bool, error)
	Snapshot(context.Context, ProductCreateCommand) (ProductSnapshot, error)
	Create(context.Context, ProductCreatePlan) error
	StoreReceipt(context.Context, ProductCreateCommand, ProductReceipt) error
	Validation() ProductValidationScope
}
type ProductCreationRepository interface {
	Replay(context.Context, ProductCreateCommand) (ProductReceipt, bool, error)
	Snapshot(context.Context, ProductCreateCommand) (ProductSnapshot, error)
	WithCreation(context.Context, func(ProductCreationScope) error) error
}
type ProductEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
	SignIsolation  func(string) (IsolationTicket, error)
	// ResumeValidation dispatches work after the creation transaction commits.
	ResumeValidation func(context.Context, string)
}
