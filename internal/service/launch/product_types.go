package launch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	gamevariant "retrom/internal/service/gamevariant"

	corevalidation "retrom/internal/core/validation"
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
type ProductSave struct {
	ID, ProfileID, GameID, SourceCoreID, PayloadID, Format, Digest string
	SizeBytes                                                      int64
	DOSEntry                                                       *string
	DiscIndex                                                      *int64
}
type (
	ProductDOSEntry struct{ Found, Safe bool }
	ProductSnapshot struct {
		Found                   bool
		Source                  gamevariant.Source
		Save                    *ProductSave
		SaveReadable            bool
		GameFiles, VariantFiles []gamevariant.File
		BIOS                    gamevariant.BIOSFacts
		ValidationBIOS          gamevariant.BIOSFacts
		DOS                     ProductDOSEntry
	}
)

type (
	ProductContentFile  struct{ FileRecord, LogicalName, Format string }
	ProductExternalFile struct{ Kind, FileRecord, LogicalName, VirtualPath string }
	ProductDisc         struct {
		FileRecord, Digest, LogicalName, VirtualPath string
		Index                                        int
		SizeBytes                                    int64
	}
)

type ProductContent struct {
	Checks []ProductBlobCheck
	Files  []ProductContentFile
	Discs  []ProductDisc
}
type ProductCreatePlan struct {
	Command                      ProductCreateCommand
	Source                       gamevariant.Source
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
	FileRecord string
	Digest     string
	SizeBytes  int64
	Exact      []byte
}
type ProductBlobVerifier interface {
	Verify(context.Context, ProductBlobCheck) error
}
type ProductCreationScope interface {
	Replay(context.Context, ProductCreateCommand) (ProductReceipt, bool, error)
	Snapshot(context.Context, ProductCreateCommand) (ProductSnapshot, error)
	Create(context.Context, ProductCreatePlan) error
	StoreReceipt(context.Context, ProductCreateCommand, ProductReceipt) error
	Validation() gamevariant.WriteScope
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
	PrepareArcade    func(context.Context, gamevariant.Snapshot) (*gamevariant.ArcadePreparation, error)
}

func (snapshot ProductSnapshot) VariantSnapshot() gamevariant.Snapshot {
	return gamevariant.Snapshot{
		Found: snapshot.Found, Source: snapshot.Source, GameFiles: snapshot.GameFiles,
		VariantFiles: snapshot.VariantFiles, BIOS: snapshot.BIOS, ValidationBIOS: snapshot.ValidationBIOS,
	}
}
