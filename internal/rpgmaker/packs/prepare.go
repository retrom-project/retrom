package packs

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/rpgmaker/fileset"
	"retrom/internal/rpgmaker/materializer"
)

type uploadSource struct {
	ID, SourceType, RelativePath, BlobID, SHA256 string
	SizeBytes                                    int64
}

type preparedInstallation struct {
	Files          []FileIdentity
	NewFileObjects []blobstore.Metadata
	Bundle         blobstore.Metadata
	FilesDigest    string
	TotalBytes     int64
}

func (service *Service) prepare(
	ctx context.Context,
	request InstallRequest,
) (preparedInstallation, error) {
	source, err := service.loadUploadSource(ctx, request.UploadID)
	if err != nil {
		return preparedInstallation{}, err
	}
	var files []FileIdentity
	var objects []blobstore.Metadata
	if source.SourceType == "DIRECTORY" {
		files, err = service.directoryFiles(ctx, request.UploadID)
	} else {
		files, objects, err = service.archiveFiles(ctx, source)
	}
	if err != nil {
		return preparedInstallation{}, err
	}
	files, err = normalizePackRoot(files, source, request.Kind)
	if err != nil {
		return preparedInstallation{}, err
	}
	result := preparedInstallation{Files: files, NewFileObjects: objects}
	identities := make([]fileset.File, 0, len(files))
	for _, file := range files {
		identities = append(identities, fileset.File{
			Role: "PROJECT_FILE", LogicalName: file.Path,
			BlobSHA256: file.SHA256, SizeBytes: file.SizeBytes,
		})
	}
	result.FilesDigest, result.TotalBytes, err = fileset.Digest(identities)
	if err != nil || result.TotalBytes > maxPackBytes {
		return preparedInstallation{}, ErrTooLarge
	}
	result.Bundle, err = service.createBundle(files)
	if err != nil {
		return preparedInstallation{}, err
	}
	return result, nil
}

func (service *Service) loadUploadSource(ctx context.Context, uploadID string) (uploadSource, error) {
	var result uploadSource
	var fileCount, incomplete, consumed int64
	err := service.database.QueryRowContext(ctx, `
SELECT session.id,session.source_type,count(file.id),
 COALESCE(sum(CASE WHEN file.state='COMPLETE' THEN 0 ELSE 1 END),0),
 EXISTS(SELECT 1 FROM upload_consumptions consumption WHERE consumption.upload_session_id=session.id)
FROM upload_sessions session
JOIN upload_files file ON file.upload_session_id=session.id
WHERE session.id=? AND session.purpose='RUNTIME_ASSET_PACK' AND session.state='COMPLETE'
GROUP BY session.id
`, uploadID).Scan(&result.ID, &result.SourceType, &fileCount, &incomplete, &consumed)
	if err != nil || incomplete != 0 || consumed != 0 || fileCount < 1 || fileCount > maxPackFiles {
		return uploadSource{}, ErrInvalid
	}
	if result.SourceType != "FILES" {
		return result, nil
	}
	err = service.database.QueryRowContext(ctx, `
SELECT file.relative_path,blob.id,blob.sha256,blob.size_bytes
FROM upload_files file JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.upload_session_id=? AND file.state='COMPLETE'
`, uploadID).Scan(&result.RelativePath, &result.BlobID, &result.SHA256, &result.SizeBytes)
	if err != nil || !archiveExtension(result.RelativePath) {
		return uploadSource{}, ErrInvalid
	}
	return result, nil
}

func (service *Service) directoryFiles(ctx context.Context, uploadID string) ([]FileIdentity, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.relative_path,blob.id,blob.size_bytes,blob.sha256
FROM upload_files file JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.upload_session_id=? AND file.state='COMPLETE'
ORDER BY file.relative_path COLLATE BINARY
`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("runtime pack directory: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack directory", rows.Close()) }()
	files := make([]FileIdentity, 0)
	for rows.Next() {
		var file FileIdentity
		if err := rows.Scan(&file.Path, &file.BlobID, &file.SizeBytes, &file.SHA256); err != nil {
			return nil, fmt.Errorf("runtime pack directory file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtime pack directory rows: %w", err)
	}
	return files, nil
}

func (service *Service) archiveFiles(
	ctx context.Context,
	source uploadSource,
) ([]FileIdentity, []blobstore.Metadata, error) {
	archivePath := service.blobs.Path(source.SHA256)
	limits := importing.DefaultArchiveLimits()
	limits.MaxEntries = maxPackFiles
	limits.MaxEntryBytes = maxPackBytes
	limits.MaxExpandedBytes = maxPackBytes
	var entries []importing.ArchiveEntry
	var err error
	if strings.EqualFold(filepath.Ext(source.RelativePath), ".zip") {
		entries, err = importing.ScanZIP(ctx, archivePath, limits)
	} else {
		entries, err = importing.ScanSevenZip(ctx, archivePath, limits)
	}
	if err != nil {
		if errors.Is(err, importing.ErrArchiveLimitExceeded) {
			return nil, nil, ErrTooLarge
		}
		return nil, nil, archivePreparationError("scan runtime pack archive", err)
	}
	files := make([]FileIdentity, 0, len(entries))
	objects := make([]blobstore.Metadata, 0, len(entries))
	for _, entry := range entries {
		metadata, extractErr := service.extractArchiveEntry(ctx, archivePath, entry)
		if extractErr != nil {
			return nil, nil, archivePreparationError("extract runtime pack archive", extractErr)
		}
		if metadata.Size != entry.Size || metadata.SHA256 != entry.SHA256 {
			return nil, nil, fmt.Errorf("%w: extract runtime pack archive", ErrInvalid)
		}
		files = append(files, FileIdentity{
			Path: entry.NormalizedPath, SizeBytes: metadata.Size, SHA256: metadata.SHA256,
		})
		objects = append(objects, metadata)
	}
	return files, objects, nil
}

func archivePreparationError(operation string, err error) error {
	if errors.Is(err, importing.ErrArchiveResourceLimit) {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, operation, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrInvalid, operation, err)
}

func (service *Service) extractArchiveEntry(
	ctx context.Context,
	archivePath string,
	entry importing.ArchiveEntry,
) (blobstore.Metadata, error) {
	if entry.ArchiveFormat == "SEVEN_Z" {
		return service.extractSevenZipEntry(ctx, archivePath, entry)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("open runtime pack ZIP: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack ZIP", reader.Close()) }()
	if entry.Ordinal < 0 || entry.Ordinal >= len(reader.File) {
		return blobstore.Metadata{}, ErrInvalid
	}
	source, err := reader.File[entry.Ordinal].Open()
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("open runtime pack ZIP entry: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack ZIP entry", source.Close()) }()
	metadata, err := service.blobs.Put(io.LimitReader(source, entry.Size+1))
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("store runtime pack ZIP entry: %w", err)
	}
	return metadata, nil
}

func (service *Service) extractSevenZipEntry(
	ctx context.Context,
	archivePath string,
	entry importing.ArchiveEntry,
) (blobstore.Metadata, error) {
	reader, writer := io.Pipe()
	finished := make(chan error, 1)
	go func() {
		err := importing.ExtractSevenZip(ctx, archivePath, entry, writer)
		finished <- err
		_ = writer.CloseWithError(err)
	}()
	metadata, putErr := service.blobs.Put(reader)
	if putErr != nil {
		_ = reader.CloseWithError(putErr)
	}
	extractErr := <-finished
	if putErr != nil || extractErr != nil {
		return blobstore.Metadata{}, errors.Join(putErr, extractErr)
	}
	return metadata, nil
}

func (service *Service) createBundle(files []FileIdentity) (blobstore.Metadata, error) {
	sources := make([]materializer.SourceFile, 0, len(files))
	for _, file := range files {
		digest := file.SHA256
		sources = append(sources, materializer.SourceFile{
			Path: file.Path, Size: file.SizeBytes,
			Open: func() (io.ReadCloser, error) { return service.blobs.OpenDigest(digest) },
		})
	}
	reader, writer := io.Pipe()
	finished := make(chan error, 1)
	go func() {
		result, err := materializer.WriteMKXPZ(writer, sources)
		if err == nil && (result.SizeBytes < 1 || len(result.SHA256) != sha256.Size*2) {
			err = materializer.ErrInvalid
		}
		finished <- err
		_ = writer.CloseWithError(err)
	}()
	metadata, putErr := service.blobs.Put(reader)
	if putErr != nil {
		_ = reader.CloseWithError(putErr)
	}
	writeErr := <-finished
	if putErr != nil || writeErr != nil {
		return blobstore.Metadata{}, errors.Join(putErr, writeErr)
	}
	return metadata, nil
}

func normalizePackRoot(
	files []FileIdentity,
	source uploadSource,
	kind string,
) ([]FileIdentity, error) {
	if len(files) == 0 || len(files) > maxPackFiles {
		return nil, ErrInvalid
	}
	strip := source.SourceType == "DIRECTORY"
	root, shared := sharedFirstComponent(files)
	if source.SourceType == "FILES" && shared && shouldStripArchiveRoot(root, kind) {
		strip = true
	}
	if strip && !shared {
		return nil, ErrInvalid
	}
	result := append([]FileIdentity(nil), files...)
	for index := range result {
		if strip {
			result[index].Path = strings.TrimPrefix(result[index].Path, root+"/")
		}
		if _, err := importing.ValidateLogicalPath(result[index].Path); err != nil {
			return nil, ErrInvalid
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Path < result[right].Path })
	return result, nil
}

func sharedFirstComponent(files []FileIdentity) (string, bool) {
	root := ""
	for _, file := range files {
		current, _, found := strings.Cut(file.Path, "/")
		if !found || current == "" {
			return "", false
		}
		if root == "" {
			root = current
		} else if root != current {
			return "", false
		}
	}
	return root, root != ""
}

func shouldStripArchiveRoot(root, kind string) bool {
	folded := foldLayoutKey(root)
	if kind == "RPG2000_RTP" || kind == "RPG2003_RTP" {
		layout, err := loadEasyRTPLayout()
		if err != nil {
			return false
		}
		generation := map[string]string{"RPG2000_RTP": "RPG2000", "RPG2003_RTP": "RPG2003"}[kind]
		_, known := foldedSet(layout.Generations[generation].Categories)[folded]
		return !known
	}
	for _, known := range []string{"audio", "fonts", "graphics"} {
		if folded == known {
			return false
		}
	}
	return true
}

func archiveExtension(value string) bool {
	extension := strings.ToLower(filepath.Ext(value))
	return extension == ".zip" || extension == ".7z"
}

func inputDigest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
