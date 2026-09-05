package libraryimport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"retrom/internal/blobstore"
	"retrom/internal/contentcapability"
	"retrom/internal/contentmanifest"
	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
)

func (service *Service) validateMultiDiscAttachmentContents(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	if err := validateMultiDiscAttachmentSet(candidate.expectedMissing, candidate.uploadFiles); err != nil {
		return err
	}
	playlist, files, err := service.multiDiscAttachmentValidationFiles(ctx, candidate)
	if err != nil {
		return err
	}
	playlistBytes, err := service.readMultiDiscAttachmentPlaylist(playlist)
	if err != nil {
		return err
	}
	parsed, err := multidisc.Parse(playlistBytes, files, multidisc.Limits{
		MaxDiscs: candidate.input.MaxDiscs, MaxTotalBytes: candidate.input.MaxTotalBytes,
	})
	if err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, err)
	}
	if !multiDiscEntriesPresent(parsed.Entries) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return multiDiscAttachmentStoreError("write canonical playlist", err)
	}
	candidate.canonicalPlaylist = canonical
	candidate.resultEntries = parsed.Entries
	return service.buildMultiDiscAttachmentManifest(candidate, playlist)
}

func (service *Service) multiDiscAttachmentValidationFiles(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) (attachedMultiDiscFile, []multidisc.File, error) {
	var playlist attachedMultiDiscFile
	files := make([]multidisc.File, 0, len(candidate.baseEntries))
	for _, file := range candidate.baseFiles {
		if file.role == "PLAYLIST_SOURCE" {
			if playlist.blobID != "" {
				return attachedMultiDiscFile{}, nil,
					multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
			}
			playlist = file
			continue
		}
		validated, err := service.multiDiscFileForValidation(ctx, candidate, file)
		if err != nil {
			return attachedMultiDiscFile{}, nil, err
		}
		files = append(files, validated)
	}
	for _, file := range candidate.uploadFiles {
		validated, err := service.multiDiscFileForValidation(ctx, candidate, file)
		if err != nil {
			return attachedMultiDiscFile{}, nil, err
		}
		files = append(files, validated)
	}
	if playlist.blobID == "" || playlist.blobSize > multidisc.MaxPlaylistBytes {
		return attachedMultiDiscFile{}, nil,
			multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return playlist, files, nil
}

func (service *Service) readMultiDiscAttachmentPlaylist(playlist attachedMultiDiscFile) ([]byte, error) {
	reader, err := service.blobs.OpenDigest(playlist.blobSHA)
	if err != nil {
		return nil, multiDiscAttachmentStoreError("open playlist", err)
	}
	playlistBytes, readErr := io.ReadAll(io.LimitReader(reader, multidisc.MaxPlaylistBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(playlistBytes)) != playlist.blobSize {
		return nil, multiDiscAttachmentStoreError("read playlist", errors.Join(readErr, closeErr))
	}
	return playlistBytes, nil
}

func multiDiscEntriesPresent(entries []multidisc.Entry) bool {
	for _, entry := range entries {
		if entry.State != multidisc.EntryPresent {
			return false
		}
	}
	return true
}

func (service *Service) buildMultiDiscAttachmentManifest(
	candidate *multiDiscAttachmentCandidate,
	playlist attachedMultiDiscFile,
) error {
	files := make([]attachedMultiDiscFile, 0, len(candidate.resultEntries)+1)
	playlist.role, playlist.sortOrder = "PLAYLIST_SOURCE", 0
	files = append(files, playlist)
	manifestFiles := make([]contentmanifest.File, 0, len(candidate.resultEntries)+1)
	manifestFiles = append(manifestFiles, contentmanifest.File{
		Role: playlist.role, LogicalName: playlist.logicalName,
		BlobSHA256: playlist.blobSHA, SizeBytes: playlist.blobSize,
	})
	for _, entry := range candidate.resultEntries {
		file := attachedMultiDiscFile{
			role: "DISC", logicalName: entry.File.LogicalName, uploadFileID: entry.File.UploadFileID,
			blobID: entry.File.BlobID, blobSHA: entry.File.BlobSHA256,
			blobSize: entry.File.SizeBytes, sortOrder: entry.Ordinal,
		}
		files = append(files, file)
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.blobSHA, SizeBytes: file.blobSize,
		})
	}
	manifest, digest, err := contentmanifest.Build(multidisc.ContentKind, manifestFiles)
	if err != nil {
		return multiDiscAttachmentStoreError("build manifest", err)
	}
	candidate.baseFiles = files
	candidate.resultManifestJSON, candidate.resultManifestDigest = string(manifest), digest
	return nil
}

func (service *Service) resolveMultiDiscAttachmentValidation(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
) ([]preparedValidationFile, string, error) {
	if len(candidate.resultEntries) < multidisc.MinDiscs {
		return nil, "", ErrInvalid
	}
	snapshot, status, code, err := corevalidation.ResolveBIOS(
		ctx, transaction, candidate.input.ProviderID, candidate.input.TargetID,
		candidate.resultEntries[0].File.LogicalName,
	)
	if err != nil {
		return nil, "", multiDiscAttachmentStoreError("resolve BIOS", err)
	}
	snapshot.MultiDisc = &corevalidation.MultiDiscSnapshot{
		ContentKind:   corevalidation.MultiDiscContentKind,
		ParserVersion: corevalidation.MultiDiscParserVersion,
		DiscCount:     len(candidate.resultEntries), MissingEntries: []corevalidation.MultiDiscMissingEntry{},
		OrderedDiscSHA256:       make([]string, 0, len(candidate.resultEntries)),
		CanonicalPlaylistSHA256: candidate.canonicalPlaylist.SHA256,
		Delivery:                corevalidation.MultiDiscDelivery,
	}
	for _, entry := range candidate.resultEntries {
		snapshot.MultiDisc.OrderedDiscSHA256 = append(
			snapshot.MultiDisc.OrderedDiscSHA256, entry.File.BlobSHA256,
		)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return nil, "", multiDiscAttachmentStoreError("encode dependency snapshot", err)
	}
	candidate.resultDependencySnapshot = snapshot
	candidate.validationStatus, candidate.compatibilityCode = status, code
	files := make([]preparedValidationFile, 0, len(snapshot.BIOS)+1)
	for _, dependency := range snapshot.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			files = append(files, preparedValidationFile{
				role: "BIOS_BUNDLE", logicalName: dependency.LogicalName,
				blobID: *dependency.BlobID, sortOrder: len(files),
			})
		}
	}
	return files, string(encoded), nil
}

func currentMultiDiscAttachmentInput(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
) (string, error) {
	var current currentMultiDiscInput
	if err := transaction.QueryRowContext(ctx, `
SELECT item.state,draft.effective_source_snapshot_id,platform.platform_id,platform.id,
platform.version,platform.default_core_id,target.provider_id,target.target_id,
json_object(
  'schemaVersion',1,
  'supportedContentKinds',json((SELECT json_group_array(content_kind) FROM (
    SELECT content_kind FROM runtime_binding_content_kinds kinds
    WHERE kinds.binding_id=runtime_binding.binding_id ORDER BY content_kind
  ))),
  'multiDisc',json_object('maxDiscs',8,'maxTotalBytes',1073741824,'delivery','EAGER_EXTERNAL_FILES')
)
FROM import_items item
JOIN review_drafts draft ON draft.id=? AND draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN runtime_target_bindings runtime_binding ON runtime_binding.core_id=platform.default_core_id
  AND runtime_binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=runtime_binding.binding_id
  AND platform_binding.platform_id=platform.platform_id
JOIN runtime_targets target ON target.provider_id=runtime_binding.provider_id
  AND target.target_id=runtime_binding.target_id
	WHERE item.id=?
	`, candidate.input.ReviewDraftID, candidate.input.ImportItemID).Scan(
		&current.itemState, &current.snapshotID, &current.platformID, &current.platformInstanceID,
		&current.platformVersion, &current.coreID, &current.providerID, &current.targetID,
		&current.contentPolicy,
	); err != nil {
		return "", multiDiscAttachmentStoreError("read current input", err)
	}
	if !current.matches(candidate.input) {
		return "", ErrInvalid
	}
	capabilities := contentcapability.Resolve(current.platformID, true, true, current.contentPolicy)
	if capabilities.MultiDisc == nil || capabilities.MultiDisc.MaxDiscs != candidate.input.MaxDiscs ||
		capabilities.MultiDisc.MaxTotalBytes != candidate.input.MaxTotalBytes {
		return "", ErrInvalid
	}
	return current.contentPolicy, nil
}

type currentMultiDiscInput struct {
	itemState, snapshotID, platformID, platformInstanceID, coreID string
	providerID, targetID, contentPolicy                           string
	platformVersion                                               int64
}

func (current currentMultiDiscInput) matches(expected multiDiscAttachmentInput) bool {
	return current.itemState == "REVIEW_PENDING" && current.snapshotID == expected.BaseSourceSnapshotID &&
		current.platformID == expected.TargetPlatformID &&
		current.platformInstanceID == expected.PlatformInstanceID &&
		current.platformVersion == expected.PlatformVersion && current.coreID == expected.CoreID &&
		current.providerID == expected.ProviderID && current.targetID == expected.TargetID &&
		validationPolicyDigest(current.contentPolicy, "MULTI_DISC") == expected.ContentPolicyDigest
}

func verifyMultiDiscAttachmentOwnership(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
) error {
	var jobState, workerID string
	if err := transaction.QueryRowContext(ctx, `SELECT state,worker_id FROM jobs WHERE id=?`, candidate.jobID).
		Scan(&jobState, &workerID); err != nil || jobState != "RUNNING" || workerID != candidate.workerID {
		return ErrInvalid
	}
	var consumed int
	if err := transaction.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM upload_consumptions
  WHERE upload_session_id=? AND upload_file_id IS NULL
)
`, candidate.input.UploadSessionID).Scan(&consumed); err != nil || consumed != 0 {
		return ErrInvalid
	}
	entries, err := loadMultiDiscEntries(ctx, transaction, candidate.input.BaseSourceSnapshotID)
	if err != nil {
		return err
	}
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil || digest != candidate.input.ExpectedSetDigest {
		return ErrInvalid
	}
	return nil
}

func insertMultiDiscSourceSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
	snapshotID string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(id,import_item_id,content_kind,
source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,'MULTI_DISC',?,?,'MULTI_DISC_ATTACHMENT',?)
`, snapshotID, candidate.input.ImportItemID,
		candidate.resultManifestJSON, candidate.resultManifestDigest, now); err != nil {
		return multiDiscAttachmentStoreError("insert source snapshot", err)
	}
	for _, file := range candidate.baseFiles {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,
upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,?,?,?,?,NULL,NULL,?,?)
`, snapshotID, file.role, file.logicalName, file.uploadFileID, file.blobID, file.sortOrder, now); err != nil {
			return multiDiscAttachmentStoreError("insert source file", err)
		}
	}
	for _, entry := range candidate.resultEntries {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_multidisc_entries(source_snapshot_id,ordinal,source_reference,
normalized_reference,canonical_name,state,upload_file_id,blob_id,source_logical_name,created_at_ms)
VALUES(?,?,?,?,?,'PRESENT',?,?,?,?)
`, snapshotID, entry.Ordinal, entry.SourceReference, entry.NormalizedReference, entry.CanonicalName,
			entry.File.UploadFileID, entry.File.BlobID, entry.File.LogicalName, now); err != nil {
			return multiDiscAttachmentStoreError("insert disc entry", err)
		}
	}
	return nil
}

func insertMultiDiscValidation(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
	snapshotID, validationID, dependencyJSON string,
	files []preparedValidationFile,
	now int64,
) error {
	canonicalBlobID, err := blobstore.EnsureRecord(
		ctx, transaction, candidate.canonicalPlaylist, "application/vnd.retrom.m3u", now,
	)
	if err != nil {
		return multiDiscAttachmentStoreError("register canonical playlist", err)
	}
	inputDigest := prepublishDigest(prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: snapshotID, SourceManifestDigest: candidate.resultManifestDigest,
		ContentKind: multidisc.ContentKind, TargetPlatformInstanceID: candidate.input.PlatformInstanceID,
		ProviderID: candidate.input.ProviderID, TargetID: candidate.input.TargetID,
		ContentPolicyDigest: candidate.input.ContentPolicyDigest,
		DependencySnapshot:  json.RawMessage(dependencyJSON), Status: candidate.validationStatus,
		CompatibilityCode: candidate.compatibilityCode,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,provider_id,target_id,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,?,?,?,NULL,NULL,?,?,?,?,?,?,?)
`, validationID, candidate.input.ImportItemID, candidate.input.PlatformInstanceID,
		candidate.input.PlatformVersion, candidate.input.CoreID, candidate.input.ProviderID,
		candidate.input.TargetID,
		candidate.resultManifestDigest, snapshotID, inputDigest,
		candidate.validationStatus, candidate.compatibilityCode, dependencyJSON, now); err != nil {
		return multiDiscAttachmentStoreError("insert validation", err)
	}
	files = append(files, preparedValidationFile{
		role: "MULTI_DISC_PLAYLIST", logicalName: "playlist.m3u", blobID: canonicalBlobID, sortOrder: 0,
	})
	for _, file := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
sort_order,created_at_ms) VALUES(?,?,?,?,?,?)
`, validationID, file.role, file.logicalName, file.blobID, file.sortOrder, now); err != nil {
			return multiDiscAttachmentStoreError("insert validation file", err)
		}
	}
	return nil
}

func recordMultiDiscDuplicateEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, platformID string,
	now int64,
) error {
	identity, err := importItemContentIdentity(ctx, transaction, itemID)
	if err != nil {
		return err
	}
	if err := claimContentIdentity(ctx, transaction, platformID, identity, now); err != nil {
		return err
	}
	duplicates, err := findDuplicateGames(ctx, transaction, itemID, platformID)
	if err != nil {
		return err
	}
	for _, game := range duplicates {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_duplicate_matches(
 import_item_id,existing_game_id,content_identity_digest,detected_stage,created_at_ms
) VALUES(?,?,?,'IDENTIFICATION',?) ON CONFLICT(import_item_id,existing_game_id) DO NOTHING
`, itemID, game.GameID, identity, now); err != nil {
			return multiDiscAttachmentStoreError("insert duplicate evidence", err)
		}
	}
	return nil
}
