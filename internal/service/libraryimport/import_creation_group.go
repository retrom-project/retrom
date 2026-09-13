package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"retrom/internal/capability/content/contentmanifest"
	"retrom/internal/capability/content/multidisc"
	"retrom/internal/service/payloadrelease"
)

func (service *ImportCreations) prepareGroup(
	group PreparedGroup,
	archives []PreparedArchive,
) (creationGroup, error) {
	record := creationGroup{group: cloneCreationGroup(group), kind: PreparedGroupContentKind(group)}
	for _, destination := range []*string{&record.itemID, &record.snapshotID, &record.validationID, &record.draftID} {
		if err := service.allocate(destination); err != nil {
			return creationGroup{}, creationError("prepare group", err)
		}
	}
	files := make([]contentmanifest.File, 0, len(group.Sources))
	for _, source := range group.Sources {
		digest, size, err := preparedSourceIdentity(source, archives)
		if err != nil {
			return creationGroup{}, creationError("prepare group", err)
		}
		var archiveSHA *string
		if source.ArchiveOrdinal != nil {
			if source.ArchiveBlobID != source.File.BlobID {
				return creationGroup{}, ErrInvalid
			}
			archiveSHA = &source.File.SHA256
		}
		files = append(files, contentmanifest.File{
			Role: source.Role, LogicalName: source.LogicalName, BlobSHA256: digest, SizeBytes: size,
			SourceArchiveSHA256: archiveSHA, SourceArchiveEntryOrdinal: source.ArchiveOrdinal,
		})
	}
	encoded, digest, err := contentmanifest.Build(record.kind, files)
	if err != nil {
		return creationGroup{}, fmt.Errorf("prepare creation manifest: %w", err)
	}
	record.manifestJSON, record.manifestDigest = string(encoded), digest
	record.groupKey, record.searchParts, err = creationGroupIdentity(group.Sources)
	if err != nil {
		return creationGroup{}, creationError("prepare group", err)
	}
	if group.GroupKey != "" {
		record.groupKey = group.GroupKey
	}
	for _, source := range group.Sources {
		record.uploadIDs = append(record.uploadIDs, source.File.ID)
	}
	slices.Sort(record.uploadIDs)
	record.uploadIDs = slices.Compact(record.uploadIDs)
	return record, nil
}

func creationGroupIdentity(sources []PreparedSource) (string, []string, error) {
	identity := make([]map[string]any, 0, len(sources))
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		identity = append(identity, map[string]any{
			"relativePath": source.File.Path, "sourceSha256": source.File.SHA256,
			"role": source.Role, "logicalName": source.LogicalName, "archiveOrdinal": source.ArchiveOrdinal,
		})
		names = append(names, source.File.Path)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", nil, fmt.Errorf("encode creation group identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), names, nil
}

func (run *creationCommit) persistGroup(
	ctx context.Context,
	scope ImportCreationScope,
	record *creationGroup,
) error {
	source, err := run.sourceChange(record)
	if err != nil {
		return creationError("persist group", err)
	}
	if err := scope.Sources.Source(ctx, source); err != nil {
		return fmt.Errorf("persist creation source: %w", err)
	}
	for _, id := range record.uploadIDs {
		run.sourceCounts[id]++
	}
	duplicate, err := run.discardDuplicate(ctx, scope, record)
	if err != nil || duplicate {
		return creationError("persist group", err)
	}
	if err := run.persistValidation(ctx, scope, record); err != nil {
		return creationError("persist group", err)
	}
	draft, err := run.draftChange(record)
	if err != nil {
		return creationError("persist group", err)
	}
	if err := scope.Reviews.Draft(ctx, draft); err != nil {
		return fmt.Errorf("persist creation draft: %w", err)
	}
	if err := run.service.tags.AssignReviewDraftTags(
		ctx, scope.Tags, record.draftID, run.tags, run.actorID, run.header.NowMS,
	); err != nil {
		return fmt.Errorf("assign creation tags: %w", err)
	}
	if err := run.persistRPG(ctx, scope, record); err != nil {
		return creationError("persist group", err)
	}
	if err := run.multidiscEvents(ctx, scope, record); err != nil {
		return creationError("persist group", err)
	}
	if run.service.scraper == nil {
		return nil
	}
	scheduled, err := run.service.scraper.ScheduleImport(
		ctx, scope.Metadata, record.itemID, run.plan.Request.MetadataProvider,
	)
	if err != nil {
		return fmt.Errorf("schedule creation metadata: %w", err)
	}
	run.scheduled = append(run.scheduled, scheduled)
	return nil
}

func (run *creationCommit) sourceChange(record *creationGroup) (CreationSource, error) {
	value := CreationSource{
		ItemID:         record.itemID,
		ImportID:       run.header.ImportID,
		SnapshotID:     record.snapshotID,
		ContentKind:    record.kind,
		GroupKey:       record.groupKey,
		State:          run.header.ItemState,
		HandoffKind:    run.options.ReviewHandoffKind,
		ManifestJSON:   record.manifestJSON,
		ManifestDigest: record.manifestDigest,
		SearchText:     strings.ToLower(strings.Join(record.searchParts, " ")),
		Discs:          record.group.MultiEntries,
		NowMS:          run.header.NowMS,
	}
	for index, source := range record.group.Sources {
		blobID := source.File.BlobID
		if source.ArchiveOrdinal != nil {
			blobID = run.materialized[source.ArchiveBlobID][*source.ArchiveOrdinal]
		}
		if blobID == "" {
			return CreationSource{}, ErrInvalid
		}
		order := index
		if source.SortOrder != nil {
			order = *source.SortOrder
		}
		value.Files = append(value.Files, CreationSourceFile{PreparedSource: source, BlobID: blobID, Order: order})
	}
	return value, nil
}

func (run *creationCommit) discardDuplicate(
	ctx context.Context,
	scope ImportCreationScope,
	record *creationGroup,
) (bool, error) {
	games, digest, err := NewContentDuplicates(scope.Duplicates).Inspect(
		ctx,
		ContentSnapshot{ID: record.snapshotID, Kind: record.kind},
		run.plan.Target.PlatformID,
	)
	if err != nil || len(games) == 0 {
		return false, creationError("discard duplicate", err)
	}
	if err := scope.Claims.ClaimIdentity(ctx, run.plan.Target.PlatformID, digest, run.header.NowMS); err != nil {
		return false, creationError("discard duplicate", err)
	}
	if err := scope.Sources.Duplicate(
		ctx,
		CreationDuplicate{ItemID: record.itemID, Identity: digest, Matches: games, NowMS: run.header.NowMS},
	); err != nil {
		return false, creationError("discard duplicate", err)
	}
	if _, err := payloadrelease.NewScheduler(nil).TerminalItem(
		ctx,
		scope.Payload,
		record.itemID,
		payloadrelease.ReasonImportDiscarded,
		run.header.NowMS,
	); err != nil {
		return false, creationError("discard duplicate", err)
	}
	run.duplicates++
	for _, id := range record.uploadIDs {
		run.duplicateCounts[id]++
	}
	return true, nil
}

func (run *creationCommit) draftChange(record *creationGroup) (CreationDraft, error) {
	titleSource := record.group.TitleSource
	if titleSource == "" && !record.group.TitleSourceExplicit {
		titleSource = record.group.Sources[0].LogicalName
	}
	title := ""
	if titleSource != "" {
		title = strings.TrimSuffix(filepath.Base(titleSource), filepath.Ext(titleSource))
	}
	metadata, err := json.Marshal(
		map[string]any{
			"title":       title,
			"description": "",
			"developer":   "",
			"publisher":   "",
			"genre":       "",
			"players":     nil,
			"releaseYear": nil,
		},
	)
	if err != nil {
		return CreationDraft{}, fmt.Errorf("encode creation draft: %w", err)
	}
	names := append([]string{record.itemID, title}, record.searchParts...)
	value := CreationDraft{
		ID:           record.draftID,
		ItemID:       record.itemID,
		TargetID:     run.plan.Target.ID,
		SnapshotID:   record.snapshotID,
		MetadataJSON: string(metadata),
		SearchText:   strings.ToLower(strings.Join(names, " ")),
		DefaultDOS:   creationOptional(record.group.DefaultDOSEntry),
		NowMS:        run.header.NowMS,
	}
	if record.group.ValidationStatus == "READY" {
		value.SelectedValidationID = &record.validationID
	}
	return value, nil
}

func creationOptional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (run *creationCommit) multidiscEvents(
	ctx context.Context,
	scope ImportCreationScope,
	record *creationGroup,
) error {
	if record.kind != multidisc.ContentKind {
		return nil
	}
	code := "MATCHED"
	if record.group.ValidationStatus != "READY" {
		code = "MISSING_DISC"
	}
	parser, err := json.Marshal(
		map[string]any{
			"schemaVersion":    1,
			"contentMode":      run.plan.ContentMode,
			"parserResultCode": code,
			"discCount":        len(record.group.MultiEntries),
		},
	)
	if err != nil {
		return fmt.Errorf("encode playlist event: %w", err)
	}
	validation, err := json.Marshal(
		map[string]any{
			"schemaVersion":     1,
			"status":            record.group.ValidationStatus,
			"compatibilityCode": record.group.CompatibilityCode,
		},
	)
	if err != nil {
		return fmt.Errorf("encode validation event: %w", err)
	}
	return creationError("multidisc events", scope.Reviews.Events(
		ctx,
		[]CreationEvent{
			{
				JobID:     run.header.JobID,
				ScopeType: "IMPORT_ITEM",
				ScopeID:   record.itemID,
				Kind:      "PLAYLIST_PARSED",
				DataJSON:  string(parser),
				NowMS:     run.header.NowMS,
			},
			{
				JobID:     run.header.JobID,
				ScopeType: "IMPORT_ITEM",
				ScopeID:   record.itemID,
				Kind:      "CORE_VALIDATION_COMPLETED",
				DataJSON:  string(validation),
				NowMS:     run.header.NowMS,
			},
		},
	))
}
