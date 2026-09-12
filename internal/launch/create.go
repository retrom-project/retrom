package launch

import (
	"context"
	"database/sql"
	"fmt"

	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"

	"retrom/internal/contentcapability"
)

type launchSelection struct {
	variantID, selectedCore                                  string
	providerID, targetID, bundleSHA256                       string
	gameID, contentLogicalName, contentKind, deliveryProfile string
	dependencySnapshotJSON, compatibilityCode                string
	contentPolicy                                            contentcapability.Policy
	datID                                                    sql.NullString
}

func validThreadCapabilities(requiresThreads bool, capabilities Capabilities) bool {
	return !requiresThreads || capabilities.SecureContext &&
		capabilities.CrossOriginIsolated && capabilities.SharedArrayBuffer
}

func (service *Service) buildProviderContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	source := application.ProductSource{
		VariantID:          selection.variantID,
		GameID:             selection.gameID,
		ProviderID:         selection.providerID,
		TargetID:           selection.targetID,
		ContentKind:        selection.contentKind,
		DeliveryProfile:    selection.deliveryProfile,
		DependencySnapshot: selection.dependencySnapshotJSON,
	}
	snapshot, err := persistence.NewProductCreation(service.database).Content(ctx, source)
	if err != nil {
		return launchContentPlan{}, fmt.Errorf("read launch content snapshot: %w", err)
	}
	content, err := application.BuildProductContent(snapshot)
	if err != nil {
		return launchContentPlan{}, fmt.Errorf("build launch content: %w", err)
	}
	verifier := productBlobVerifier{blobs: service.blobs}
	for _, check := range content.Checks {
		if err := verifier.Verify(ctx, check); err != nil {
			return launchContentPlan{}, err
		}
	}
	return launchContentPlan{ContentKind: selection.contentKind, Files: content.Files, Discs: content.Discs}, nil
}

func (service *Service) lockVariantBundleFiles(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, variantID string,
	now int64,
) error {
	repository := persistence.NewProductExternals(transaction)
	files, err := repository.Bundles(ctx, variantID)
	if err != nil {
		return fmt.Errorf("read variant bundle files: %w", err)
	}
	if err := repository.Store(ctx, launchID, application.ProductBundleFiles(files), now); err != nil {
		return fmt.Errorf("lock variant bundle files: %w", err)
	}
	return nil
}

func (service *Service) lockExternalBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, variantID string,
	now int64,
	allowMissing bool,
) error {
	repository := persistence.NewProductExternals(transaction)
	snapshot, found, err := repository.Snapshot(ctx, launchID, variantID)
	if err != nil {
		return fmt.Errorf("read launch external snapshot: %w", err)
	}
	if !found {
		return ErrBlocked
	}
	files, err := application.FreezeProductExternalBIOS(snapshot, allowMissing)
	if err != nil {
		return fmt.Errorf("prepare launch external BIOS: %w", err)
	}
	if err := repository.Store(ctx, launchID, files, now); err != nil {
		return fmt.Errorf("lock launch external BIOS: %w", err)
	}
	return nil
}
