package launch

import (
	"context"
	"reflect"

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
	snapshot.Source, snapshot.Found, err = productCreationSource(ctx, records.executor, command, snapshot.Save)
	if err != nil || !snapshot.Found {
		return snapshot, err
	}
	return records.completeSnapshot(ctx, command, snapshot)
}

func (records productCreationRecords) completeSnapshot(
	ctx context.Context,
	command application.ProductCreateCommand,
	snapshot application.ProductSnapshot,
) (application.ProductSnapshot, error) {
	var err error
	snapshot.GameFiles, err = productCreationFiles(ctx, records.executor, snapshot.Source.GameID, false)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	snapshot.VariantFiles, err = productCreationFiles(ctx, records.executor, snapshot.Source.VariantID, true)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	productSourceNames(&snapshot.Source, snapshot.GameFiles)
	snapshot.BIOS, err = ProductBIOSFacts(ctx, records.executor, snapshot.Source, snapshot.Source.DATVersionID)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	snapshot.ValidationBIOS = snapshot.BIOS
	if !reflect.DeepEqual(snapshot.Source.DATVersionID, snapshot.Source.ActiveDATVersionID) {
		snapshot.ValidationBIOS, err = ProductBIOSFacts(
			ctx,
			records.executor,
			snapshot.Source,
			snapshot.Source.ActiveDATVersionID,
		)
		if err != nil {
			return application.ProductSnapshot{}, err
		}
	}
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
