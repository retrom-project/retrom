package launch

import (
	"context"
	"fmt"

	variantrepository "retrom/internal/persistence/gamevariant"

	application "retrom/internal/service/launch"
)

func (records productCreationRecords) Snapshot(
	ctx context.Context,
	command application.ProductCreateCommand,
) (application.ProductSnapshot, error) {
	allowed, err := productCreationOwner(ctx, records.executor, command)
	if err != nil || !allowed {
		return application.ProductSnapshot{}, err
	}
	var snapshot application.ProductSnapshot
	snapshot.Save, snapshot.SaveReadable, err = productCreationSave(ctx, records.executor, command)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	if command.Request.SaveStateID != nil && snapshot.Save == nil {
		return snapshot, nil
	}
	coreID := ""
	if command.Request.CoreID != nil {
		coreID = *command.Request.CoreID
	} else if snapshot.Save != nil {
		coreID = snapshot.Save.SourceCoreID
	}
	configuration, err := variantrepository.ReadSnapshot(ctx, records.executor, command.Request.GameID, coreID)
	if err != nil {
		return snapshot, fmt.Errorf("read game configuration: %w", err)
	}
	if !configuration.Found {
		return snapshot, nil
	}
	snapshot.Found, snapshot.Source = configuration.Found, configuration.Source
	snapshot.GameFiles, snapshot.VariantFiles = configuration.GameFiles, configuration.VariantFiles
	snapshot.BIOS, snapshot.ValidationBIOS = configuration.BIOS, configuration.ValidationBIOS
	entry := command.Request.DOSEntry
	if snapshot.Save != nil && snapshot.Save.DOSEntry != nil {
		entry = snapshot.Save.DOSEntry
	}
	snapshot.DOS, err = productCreationDOS(ctx, records.executor, snapshot.Source.GameID, entry)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	return snapshot, nil
}
