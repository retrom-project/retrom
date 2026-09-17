package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	validationpersistence "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/dbexec"
	librarypersistence "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	libraryimportmodel "retrom/internal/model/libraryimport"
)

func prepareStaticBIOSDependencies(
	ctx context.Context,
	transaction *sql.Tx,
	providerID, targetID, platformID string,
	groups []preparedGroup,
) error {
	if err := libraryimportservice.PrepareCreationStaticBIOS(ctx, validationpersistence.New(transaction),
		libraryimportmodel.ImportTarget{ProviderID: providerID, TargetID: targetID, PlatformID: platformID}, groups); err != nil {
		return fmt.Errorf("prepare static BIOS: %w", err)
	}
	return nil
}

type (
	Approved         = libraryimportmodel.ReviewApproved
	ApprovalDecision = libraryimportmodel.ReviewApprovalDecision
	ExternalAsset    = libraryimportmodel.ApprovalExternalAsset
)

func (service *Service) validateCurrentApprovalDependencySnapshot(
	ctx context.Context, transaction dbexec.Executor, sourceSnapshotID, validationID, platformID, providerID,
	targetID string,
	policy contentcapability.Policy, contentKind, frozenJSON string,
) error {
	err := libraryimportservice.ValidateApprovalDependencies(ctx,
		librarypersistence.BindApprovalDependencies(transaction), libraryimportmodel.ApprovalDependencyInput{
			SnapshotID: sourceSnapshotID, ValidationID: validationID, PlatformID: platformID,
			ProviderID: providerID, TargetID: targetID,
			Policy: policy, ContentKind: contentKind, DependencyJSON: frozenJSON,
		})
	if err != nil {
		return fmt.Errorf("validate approval dependencies: %w", err)
	}
	return nil
}

type approvalValidationDigestInput struct {
	VariantID, ContentID, ContentKind, ProviderID, TargetID string
	ContentPolicy                                           contentcapability.Policy
	DATID                                                   sql.NullString
	ValidationID                                            string
	Snapshot                                                corevalidation.Snapshot
	SnapshotValid                                           bool
}

func approvalValidationInputDigest(input approvalValidationDigestInput) (string, error) {
	if !input.SnapshotValid {
		if input.ContentKind != contentcapability.ModeRPGMakerProject {
			validationDigest := sha256.Sum256([]byte(input.ValidationID))
			return hex.EncodeToString(validationDigest[:]), nil
		}
		input.Snapshot = corevalidation.Snapshot{
			SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindStatic,
			BIOS: []corevalidation.BIOSDependency{},
		}
	}
	if input.ContentKind != multidisc.ContentKind {
		digest, err := corevalidation.ProviderValidationInputDigest(
			input.ProviderID, input.TargetID, input.ContentID, dbexec.StringPointer(input.DATID),
			input.Snapshot,
		)
		if err != nil {
			return "", fmt.Errorf("libraryimport/service: %w", err)
		}
		return digest, nil
	}
	if input.Snapshot.MultiDisc == nil {
		return "", ErrInvalid
	}
	biosDigest, err := corevalidation.BIOSDependencyDigest(input.Snapshot)
	if err != nil {
		return "", ErrInvalid
	}
	digest, err := corevalidation.MultiDiscValidationInputDigest(corevalidation.MultiDiscValidationInput{
		GameVariantID: input.VariantID, GameID: input.ContentID,
		ContentKind: input.ContentKind, ProviderID: input.ProviderID, TargetID: input.TargetID,
		ContentPolicySHA256: input.ContentPolicy.Digest(),
		DATVersionID:        dbexec.StringPointer(input.DATID), BIOSDependencySHA256: biosDigest,
		OrderedDiscSHA256:       input.Snapshot.MultiDisc.OrderedDiscSHA256,
		CanonicalPlaylistSHA256: input.Snapshot.MultiDisc.CanonicalPlaylistSHA256,
	})
	if err != nil {
		return "", fmt.Errorf("libraryimport/service: %w", err)
	}
	return digest, nil
}
