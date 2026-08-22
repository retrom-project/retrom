package libraryimport

import (
	"context"
	"database/sql"
	"fmt"
)

func (service *Service) Create(ctx context.Context, request CreateRequest) (Created, error) {
	return service.create(ctx, request, nil)
}

func resolveInitialArcadeBIOSState(
	ctx context.Context,
	transaction *sql.Tx,
	platformID, artifactID string,
	group *preparedGroup,
	status, code, snapshotJSON string,
) (string, string, string, error) {
	if platformID != "arcade" {
		return status, code, snapshotJSON, nil
	}
	biosState, err := resolveArcadeDraftBIOSState(ctx, transaction, artifactID, snapshotJSON, status, code)
	if err != nil {
		return "", "", "", err
	}
	if !biosState.tracked {
		return status, code, snapshotJSON, nil
	}
	for _, dependency := range biosState.dependencies {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		group.validationFiles = append(group.validationFiles, preparedValidationFile{
			role:        "BIOS_BUNDLE",
			logicalName: dependency.LogicalName,
			blobID:      *dependency.BlobID,
			sortOrder:   len(group.validationFiles),
		})
	}
	return biosState.status, biosState.code, biosState.snapshotJSON, nil
}

func (service *Service) create(
	ctx context.Context,
	request CreateRequest,
	reconfiguration *reconfigurationInput,
) (Created, error) {
	plan, err := service.prepareCreation(ctx, request)
	if err != nil {
		return Created{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	run := newCreationRun(ctx, service, transaction, plan, reconfiguration)
	if err := run.execute(); err != nil {
		return Created{}, err
	}
	return run.result(), nil
}
