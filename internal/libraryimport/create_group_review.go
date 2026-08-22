package libraryimport

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/multidisc"
)

func (run *creationRun) persistReviewDraft(record *groupRecord) error {
	titleSource := record.group.titleSource
	if titleSource == "" {
		titleSource = record.group.sources[0].logicalName
	}
	title := strings.TrimSuffix(filepath.Base(titleSource), filepath.Ext(titleSource))
	metadataJSON, _ := json.Marshal(map[string]any{
		"title": title, "description": "", "developer": "", "publisher": "", "genre": "",
		"players": nil, "releaseYear": nil,
	})
	searchParts := append([]string{record.itemID, title}, record.searchParts...)
	if _, err := run.transaction.ExecContext(run.ctx, `
UPDATE import_items SET search_text=? WHERE id=?
`, strings.ToLower(strings.Join(searchParts, " ")), record.itemID); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	var selectedValidation any
	if record.validationStatus == "READY" {
		selectedValidation = record.validationID
	}
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_drafts(
  id,import_item_id,target_platform_instance_id,selected_validation_id,effective_source_snapshot_id,
  default_dos_entry,metadata_json,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,1,?,?)
`, record.draftID, record.itemID, run.plan.request.TargetPlatformInstanceID, selectedValidation,
		record.sourceSnapshotID, nullableText(record.group.defaultDOSEntry), string(metadataJSON), run.now, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := run.service.tags.AssignReviewDraftTags(
		run.ctx, run.transaction, record.draftID, run.tagReferences, run.actorUserID, run.now,
	); err != nil {
		return fmt.Errorf("libraryimport/service: assign draft tags: %w", err)
	}
	return nil
}

func (run *creationRun) persistMultiDiscEvents(record *groupRecord) error {
	if record.contentKind != multidisc.ContentKind {
		return nil
	}
	parserResultCode := "MATCHED"
	if record.validationStatus != "READY" {
		parserResultCode = "MISSING_DISC"
	}
	parserData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "contentMode": run.plan.contentMode,
		"parserResultCode": parserResultCode, "discCount": len(record.group.multiEntries),
	})
	validationData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "status": record.validationStatus,
		"compatibilityCode": record.compatibilityCode,
	})
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'PLAYLIST_PARSED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?)
`, run.jobID, record.itemID, string(parserData), run.now,
		run.jobID, record.itemID, string(validationData), run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *creationRun) scheduleMetadata(record *groupRecord) error {
	if run.service.scraper == nil {
		return nil
	}
	scheduled, err := run.service.scraper.ScheduleImport(
		run.ctx, run.transaction, record.itemID, run.plan.request.MetadataProvider,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	run.scheduledRuns = append(run.scheduledRuns, scheduled)
	return nil
}
