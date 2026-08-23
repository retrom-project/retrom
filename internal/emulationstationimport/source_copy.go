package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
)

func (service *Service) executionSourceFiles(
	ctx context.Context,
	unit work,
	root Root,
	item executionItem,
) ([]libraryimport.ServerSourceFile, error) {
	files := make([]libraryimport.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(
			files,
			libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: file.BlobID, SizeBytes: file.Size},
		)
	}
	arcadeArchive := item.TargetPlatformKind == "arcade" && len(item.Files) == 1 &&
		strings.EqualFold(path.Ext(item.Files[0].Path), ".zip")
	if !arcadeArchive {
		return files, nil
	}
	companions, err := service.arcadeCompanions(ctx, unit, root, item)
	if err != nil {
		return nil, err
	}
	return append(files, companions...), nil
}

func (service *Service) recordCopiedFile(
	ctx context.Context,
	itemID string,
	ordinal int64,
	metadata blobstore.Metadata,
) (string, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("emulationstationimport/start copied file transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, "application/octet-stream", now)
	if err != nil {
		return "", fmt.Errorf("emulationstationimport/record copied file blob: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_item_files
SET blob_id=?,state='COPIED',updated_at_ms=?
WHERE item_id=? AND ordinal=? AND state='DISCOVERED'`, blobID, now, itemID, ordinal)
	if err != nil {
		return "", fmt.Errorf("emulationstationimport/record copied file: %w", err)
	}
	if rowsAffected(result) != 1 {
		return "", fmt.Errorf("emulationstationimport/record copied file: %w", errItemStateChanged)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("emulationstationimport/commit copied file: %w", err)
	}
	return blobID, nil
}

func (service *Service) recordCopiedAsset(
	ctx context.Context,
	itemID, kind string,
	metadata blobstore.Metadata,
	mediaType string,
) (string, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("emulationstationimport/start copied asset transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, mediaType, now)
	if err != nil {
		return "", fmt.Errorf("emulationstationimport/record copied asset blob: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_item_assets
SET blob_id=?,state='COPIED',updated_at_ms=?
WHERE item_id=? AND kind=? AND state='DISCOVERED'`, blobID, now, itemID, kind)
	if err != nil {
		return "", fmt.Errorf("emulationstationimport/record copied asset: %w", err)
	}
	if rowsAffected(result) != 1 {
		return "", fmt.Errorf("emulationstationimport/record copied asset: %w", errItemStateChanged)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("emulationstationimport/commit copied asset: %w", err)
	}
	return blobID, nil
}

func selectServerImportItem(
	items []libraryimport.ServerImportItem,
	source []executionFile,
) (libraryimport.ServerImportItem, bool) {
	if len(source) == 0 {
		return libraryimport.ServerImportItem{}, false
	}
	wanted := make(map[string]struct{}, len(source))
	for _, file := range source {
		wanted[file.Path] = struct{}{}
	}
	var selected libraryimport.ServerImportItem
	found := false
	for _, item := range items {
		matches := false
		for _, relativePath := range item.SourceRelativePaths {
			if _, exists := wanted[relativePath]; exists {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		if found {
			return libraryimport.ServerImportItem{}, false
		}
		selected, found = item, true
	}
	return selected, found
}

func (service *Service) arcadeCompanions(
	ctx context.Context,
	unit work,
	root Root,
	item executionItem,
) ([]libraryimport.ServerSourceFile, error) {
	candidates, err := service.arcadeCompanionCandidates(ctx, unit.ImportID, item)
	if err != nil {
		return nil, err
	}
	result := make([]libraryimport.ServerSourceFile, 0, len(candidates))
	for _, file := range candidates {
		metadata, err := service.copySource(
			ctx,
			root,
			unit.ImportID,
			unit.RelativePath,
			file.Path,
			file.Size,
			file.Facts,
		)
		if err != nil {
			if errors.Is(err, errImportCancelled) {
				return nil, err
			}
			continue
		}
		transaction, err := service.database.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("emulationstationimport/start companion transaction: %w", err)
		}
		blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, "application/zip", service.now().UnixMilli())
		if err == nil {
			err = transaction.Commit()
		} else {
			cleanup.Rollback(transaction)
		}
		if err != nil {
			return nil, fmt.Errorf("emulationstationimport/record arcade companion: %w", err)
		}
		result = append(
			result,
			libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: blobID, SizeBytes: file.Size},
		)
	}
	return result, nil
}

func (service *Service) arcadeCompanionCandidates(
	ctx context.Context,
	importID string,
	item executionItem,
) ([]executionFile, error) {
	if item.TargetDATVersionID == "" || len(item.Files) != 1 {
		return []executionFile{}, nil
	}
	machine := strings.TrimSuffix(path.Base(item.Files[0].Path), path.Ext(item.Files[0].Path))
	dependencies, err := service.arcadeDependencyMachines(ctx, item.TargetDATVersionID, machine)
	if err != nil {
		return nil, err
	}
	if len(dependencies) == 0 {
		return []executionFile{}, nil
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.relative_path,file.size_bytes,file.source_facts_digest
FROM emulationstation_import_items candidate
JOIN emulationstation_import_collections collection ON collection.id=candidate.collection_id
JOIN emulationstation_import_item_files file ON file.item_id=candidate.id
WHERE candidate.import_id=? AND candidate.id<>? AND candidate.discovery_state='READY'
AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
AND collection.target_dat_version_id=?
AND (SELECT count(*) FROM emulationstation_import_item_files own WHERE own.item_id=candidate.id)=1
ORDER BY file.relative_path`, importID, item.ID, item.TargetPlatformID, item.TargetDATVersionID)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/query arcade companions: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]executionFile, 0)
	for rows.Next() {
		var file executionFile
		if err := rows.Scan(&file.Path, &file.Size, &file.Facts); err != nil {
			return nil, fmt.Errorf("emulationstationimport/scan arcade companion: %w", err)
		}
		if !strings.EqualFold(path.Ext(file.Path), ".zip") {
			continue
		}
		candidateMachine := strings.TrimSuffix(path.Base(file.Path), path.Ext(file.Path))
		if _, required := dependencies[candidateMachine]; !required {
			continue
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate arcade companions: %w", err)
	}
	return result, nil
}

func (service *Service) arcadeDependencyMachines(
	ctx context.Context,
	datVersionID, machine string,
) (map[string]struct{}, error) {
	rows, err := service.database.QueryContext(ctx, `
WITH RECURSIVE dependency(machine) AS (
 SELECT cloneof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND cloneof IS NOT NULL
 UNION
 SELECT romof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND romof IS NOT NULL
 UNION
 SELECT relation.cloneof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.cloneof IS NOT NULL
 UNION
 SELECT relation.romof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.romof IS NOT NULL
)
SELECT machine FROM dependency WHERE machine<>? ORDER BY machine`,
		datVersionID, machine, datVersionID, machine, datVersionID, datVersionID, machine,
	)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/query arcade dependency closure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make(map[string]struct{})
	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err != nil {
			return nil, fmt.Errorf("emulationstationimport/scan arcade dependency closure: %w", err)
		}
		result[dependency] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate arcade dependency closure: %w", err)
	}
	return result, nil
}

func terminalForCode(code string) string {
	if code == "EMULATIONSTATION_SOURCE_CHANGED" {
		return "SOURCE_CHANGED"
	}
	return "READ_FAILED"
}

func (service *Service) copySource(
	ctx context.Context,
	root Root,
	importID string,
	selectedPath, relativePath string,
	size int64,
	facts string,
) (blobstore.Metadata, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("emulationstationimport/acquire source reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, relativePath)
	if err != nil || before.Size() != size || serversource.FactsDigest(before) != facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, ErrSourceChanged
	}
	metadata, putErr := service.blobs.Put(&contextReader{
		ctx:       ctx,
		reader:    io.LimitReader(handle, size+1),
		cancelled: func() bool { return service.importCancelled(ctx, importID) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, fmt.Errorf("emulationstationimport/copy source to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != facts {
		return blobstore.Metadata{}, ErrSourceChanged
	}
	return metadata, nil
}

func (service *Service) copyAsset(
	ctx context.Context,
	root Root,
	importID string,
	selectedPath string,
	asset executionAsset,
) (blobstore.Metadata, bool, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("emulationstationimport/acquire asset reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, asset.Path)
	if err != nil || before.Size() != asset.Size || serversource.FactsDigest(before) != asset.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, false, ErrSourceChanged
	}
	valid := copiedAssetValid(handle, asset)
	if !valid {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, nil
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, fmt.Errorf("emulationstationimport/rewind asset: %w", err)
	}
	metadata, putErr := service.blobs.Put(&contextReader{
		ctx:       ctx,
		reader:    io.LimitReader(handle, asset.Size+1),
		cancelled: func() bool { return service.importCancelled(ctx, importID) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("emulationstationimport/copy asset to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != asset.Size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != asset.Facts {
		return blobstore.Metadata{}, false, ErrSourceChanged
	}
	return metadata, true, nil
}

func copiedAssetValid(handle io.ReadSeeker, asset executionAsset) bool {
	if asset.Kind == "COVER" {
		image, err := mediaasset.InspectImage(handle, asset.Size)
		return err == nil && image.MediaType == asset.MediaType &&
			asset.Width.Valid && asset.Height.Valid &&
			image.WidthPX == asset.Width.Int64 && image.HeightPX == asset.Height.Int64
	}
	mediaType, err := mediaasset.InspectVideo(handle, asset.Size)
	return err == nil && mediaType == asset.MediaType
}
