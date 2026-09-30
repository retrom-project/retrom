package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	validationpersistence "retrom/internal/persistence/corevalidation"
	libraryservice "retrom/internal/service/libraryimport"
)

func prepareStaticBIOSDependencies(
	ctx context.Context,
	transaction dbapi.Tx,
	providerID, targetID, platformID string,
	groups []preparedGroup,
) error {
	if err := libraryservice.PrepareCreationStaticBIOS(ctx, validationpersistence.New(transaction),
		libraryservice.ImportTarget{ProviderID: providerID, TargetID: targetID, PlatformID: platformID}, groups); err != nil {
		return fmt.Errorf("prepare static BIOS: %w", err)
	}
	return nil
}

type (
	Approved         = libraryservice.ReviewApproved
	ApprovalDecision = libraryservice.ReviewApprovalDecision
	ExternalAsset    = libraryservice.ApprovalExternalAsset
)
