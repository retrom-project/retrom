package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	librarypersistence "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/contentmanifest"
	"retrom/internal/importing"
)

// Claim, scan, exact DAT match, revalidation, and atomic commit form one worker contract.
func (service *Service) runParentAttachment(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, parentAttachmentDeadline)
	defer cancel()
	candidate, workerID, err := service.claimParentAttachment(ctx, jobID)
	if err != nil {
		return
	}
	archive, ok := service.validateParentArchive(ctx, candidate, jobID, workerID)
	if !ok {
		return
	}
	commit, ok := service.prepareParentCommit(ctx, candidate, jobID, workerID)
	if !ok {
		return
	}
	if err := service.commitAcceptedParentAttachment(
		ctx, candidate, jobID, workerID, archive.entries, commit.files,
		commit.manifestJSON, commit.manifestDigest, commit.group, archive.diagnostics,
	); err != nil {
		if service.finishParentAttachmentCancellation(ctx, candidate, jobID, workerID) {
			return
		}
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorInputStale)
	}
}

type validatedParentArchive struct {
	entries     []importing.ArchiveEntry
	diagnostics map[string]any
}

func (service *Service) validateParentArchive(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) (validatedParentArchive, bool) {
	entries, err := importing.ScanZIP(
		ctx, service.blobs.Path(candidate.blobSHA), importing.DefaultArchiveLimits(),
	)
	if err != nil {
		code := ParentErrorArchiveUnsafe
		if errors.Is(err, importing.ErrNestedArchiveUnsupported) {
			code = ParentErrorStructure
		}
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID, code, archiveReason(err), nil, nil,
		)
		return validatedParentArchive{}, false
	}
	entryByName, ignoredNestedEntries := rootParentEntries(entries)
	if len(entryByName) == 0 {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID,
			ParentErrorStructure, "ROOT_ROM_ENTRIES_MISSING", nil, nil,
		)
		return validatedParentArchive{}, false
	}
	requirements, hasDisk, err := service.arcadeRequirements(ctx, candidate.datID, candidate.machine)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return validatedParentArchive{}, false
	}
	if hasDisk {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID, ParentErrorStructure, "UNSUPPORTED_CHD", nil, nil,
		)
		return validatedParentArchive{}, false
	}
	missing, mismatched, warnings := matchArcadeRequirements(entryByName, requirements)
	if len(missing) != 0 || len(mismatched) != 0 {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID,
			ParentErrorMismatch, "DAT_ENTRY_MISMATCH", missing, mismatched,
		)
		return validatedParentArchive{}, false
	}
	return validatedParentArchive{
		entries: entries,
		diagnostics: map[string]any{
			"schemaVersion": 1, "requiredEntryCount": len(requirements),
			"observedEntryCount": len(entries), "observedRootEntryCount": len(entryByName),
			"ignoredNestedEntryCount": ignoredNestedEntries, "warnings": warnings,
		},
	}, true
}

type preparedParentCommit struct {
	files          []attachedSourceFile
	manifestJSON   string
	manifestDigest string
	group          preparedGroup
}

func (service *Service) prepareParentCommit(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) (preparedParentCommit, bool) {
	files, manifestJSON, manifestDigest, err := service.buildAttachedSourceSnapshot(ctx, candidate)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	preparedFiles := make([]importSourceFile, 0, len(files))
	for _, file := range files {
		preparedFiles = append(preparedFiles, importSourceFile{
			ID: file.uploadFileID, Path: file.logicalName, BlobID: file.blobID, SHA256: file.blobSHA,
		})
	}
	_, groups, _, preparationErr := service.prepareArcadeFiles(
		ctx, preparedFiles, sql.NullString{String: candidate.datID, Valid: true},
	)
	if preparationErr != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	rootMachine, err := service.parentAttachmentRootMachine(ctx, candidate)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	group := selectParentGroup(groups, rootMachine)
	if group == nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	return preparedParentCommit{
		files: files, manifestJSON: manifestJSON, manifestDigest: manifestDigest, group: *group,
	}, true
}

func selectParentGroup(groups []preparedGroup, rootMachine string) *preparedGroup {
	for index := range groups {
		for _, source := range groups[index].Sources {
			if source.Role == "CONTENT" && source.LogicalName == rootMachine+".zip" {
				return &groups[index]
			}
		}
	}
	return nil
}

func rootParentEntries(entries []importing.ArchiveEntry) (map[string]importing.ArchiveEntry, int) {
	rootEntries := make(map[string]importing.ArchiveEntry, len(entries))
	ignoredNestedEntries := 0
	for _, entry := range entries {
		if strings.Contains(entry.NormalizedPath, "/") {
			ignoredNestedEntries++
			continue
		}
		rootEntries[entry.NormalizedPath] = entry
	}
	return rootEntries, ignoredNestedEntries
}

func (service *Service) parentAttachmentRootMachine(
	ctx context.Context,
	candidate parentAttachmentCandidate,
) (string, error) {
	raw, err := librarypersistence.NewArcadeParentAttachmentWorker(service.database).RootValidation(
		ctx, application.ArcadeParentAttachmentCandidate{
			ItemID: candidate.itemID, BaseSnapshotID: candidate.baseSnapshotID,
			ProviderID: candidate.providerID, TargetID: candidate.targetID, DATID: candidate.datID,
		},
	)
	if err != nil {
		return "", parentStoreError("read root validation", err)
	}
	snapshot, valid := parseArcadeDraftSnapshot(raw)
	if !valid {
		return "", ErrInvalid
	}
	return snapshot.Machine, nil
}

func (service *Service) claimParentAttachment(
	ctx context.Context,
	jobID string,
) (parentAttachmentCandidate, string, error) {
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	claim, err := librarypersistence.NewArcadeParentAttachmentWorker(service.database).Claim(
		ctx, jobID, workerID.String(), now,
	)
	if err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("claim", err)
	}
	return parentAttachmentCandidateFromApplication(claim.Candidate), claim.WorkerID, nil
}

func parentAttachmentCandidateFromApplication(
	candidate application.ArcadeParentAttachmentCandidate,
) parentAttachmentCandidate {
	return parentAttachmentCandidate{
		attachmentID: candidate.AttachmentID, itemID: candidate.ItemID, draftID: candidate.DraftID,
		baseSnapshotID: candidate.BaseSnapshotID, machine: candidate.Machine, requiredBy: candidate.RequiredBy,
		providerID: candidate.ProviderID, targetID: candidate.TargetID, datID: candidate.DATID,
		uploadFileID: candidate.UploadFileID, uploadSessionID: candidate.UploadSessionID,
		originalName: candidate.OriginalName, blobID: candidate.BlobID, blobSHA: candidate.BlobSHA,
		blobSize: candidate.BlobSize, contentPolicyDigest: candidate.ContentPolicyDigest, depth: candidate.Depth,
	}
}

type attachedSourceFile struct {
	role, logicalName, uploadFileID, blobID, blobSHA string
	blobSize                                         int64
	archiveBlobID                                    sql.NullString
	archiveOrdinal                                   sql.NullInt64
	archiveSHA                                       string
	sortOrder                                        int
}

func (service *Service) buildAttachedSourceSnapshot(
	ctx context.Context,
	candidate parentAttachmentCandidate,
) ([]attachedSourceFile, string, string, error) {
	snapshotFiles, err := librarypersistence.NewArcadeParentAttachmentWorker(service.database).SourceSnapshot(
		ctx, candidate.baseSnapshotID,
	)
	if err != nil {
		return nil, "", "", parentStoreError("read source snapshot", err)
	}
	files := make([]attachedSourceFile, 0)
	logicalName := candidate.machine + ".zip"
	replaced := false
	for _, source := range snapshotFiles {
		file := attachedSourceFile{
			role: source.Role, logicalName: source.LogicalName, uploadFileID: source.UploadFileID,
			blobID: source.BlobID, blobSHA: source.BlobSHA, blobSize: source.BlobSize,
			archiveSHA: source.SourceArchiveSHA,
		}
		if source.SourceArchiveBlobID != "" {
			file.archiveBlobID = sql.NullString{String: source.SourceArchiveBlobID, Valid: true}
			if source.SourceArchiveEntryOrdinal != nil {
				file.archiveOrdinal = sql.NullInt64{Int64: int64(*source.SourceArchiveEntryOrdinal), Valid: true}
			}
		}
		if importing.ASCIICaseFold(file.logicalName) == importing.ASCIICaseFold(logicalName) {
			if file.role != "COMPANION" {
				return nil, "", "", ErrInvalid
			}
			file = attachedSourceFile{
				role: "COMPANION", logicalName: logicalName, uploadFileID: candidate.uploadFileID,
				blobID: candidate.blobID, blobSHA: candidate.blobSHA, blobSize: candidate.blobSize,
			}
			replaced = true
		}
		files = append(files, file)
	}
	if !replaced {
		files = append(files, attachedSourceFile{
			role: "COMPANION", logicalName: logicalName, uploadFileID: candidate.uploadFileID,
			blobID: candidate.blobID, blobSHA: candidate.blobSHA, blobSize: candidate.blobSize,
		})
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].role != files[right].role {
			return files[left].role < files[right].role
		}
		return files[left].logicalName < files[right].logicalName
	})
	manifestFiles := make([]contentmanifest.File, 0, len(files))
	for index := range files {
		file := &files[index]
		file.sortOrder = index
		manifest := contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.blobSHA, SizeBytes: file.blobSize,
		}
		if file.archiveBlobID.Valid {
			ordinal := int(file.archiveOrdinal.Int64)
			manifest.SourceArchiveSHA256 = &file.archiveSHA
			manifest.SourceArchiveEntryOrdinal = &ordinal
		}
		manifestFiles = append(manifestFiles, manifest)
	}
	contents, digest, err := contentmanifest.Build("SINGLE_FILE", manifestFiles)
	return files, string(contents), digest, err
}
