package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records scanRecords) Headers(
	ctx context.Context,
	change application.ScanMutation,
	result application.ScanProjection,
) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	for _, gamelist := range result.Gamelists {
		ignoredFields := gamelist.Document.IgnoredFields
		if ignoredFields == nil {
			ignoredFields = []string{}
		}
		ignoredBytes, err := json.Marshal(ignoredFields)
		if err != nil {
			return fmt.Errorf("encode ignored scan fields: %w", err)
		}
		ignoredJSON := string(ignoredBytes)
		writeResult, err := recordstore.CreateEmulationstationImportGamelists(ctx, records.executor, `
INSERT INTO emulationstation_import_gamelists(
import_id,relative_path,size_bytes,content_digest,source_facts_digest,
parse_state,error_code,game_count,folder_count,provider_present,
ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			change.Before.ImportID, gamelist.Path, gamelist.Size, optionalText(gamelist.Digest),
			gamelist.Facts, gamelist.State, optionalText(gamelist.ErrorCode),
			len(gamelist.Document.Games), gamelist.Document.FolderEntryCount,
			scanBoolInt(gamelist.Document.ProviderPresent), ignoredJSON,
			gamelist.Document.IgnoredFieldOtherCount, change.NowMS)
		if err := requireWorkflowChange(writeResult, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("emulationstationimport/insert gamelist evidence: %w", err)
		}
	}
	for _, collection := range result.Collections {
		writeResult, err := recordstore.CreateEmulationstationImportCollections(ctx, records.executor, `
INSERT INTO emulationstation_import_collections(
id,import_id,gamelist_relative_path,relative_directory,display_name,
game_count,issue_count,folder_entry_count,hidden_game_count,adult_game_count,
extension_summary_json,extension_other_count,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, collection.ID, change.Before.ImportID, collection.GamelistPath,
			collection.RelativeDirectory, collection.DisplayName, collection.GameCount, collection.IssueCount,
			collection.FolderEntryCount, collection.HiddenGameCount, collection.AdultGameCount,
			collection.ExtensionSummaryJSON, collection.ExtensionOtherCount, change.NowMS, change.NowMS)
		if err := requireWorkflowChange(writeResult, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("emulationstationimport/insert collection: %w", err)
		}
	}
	return nil
}

func scanBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
