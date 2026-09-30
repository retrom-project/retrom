package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/launch"
)

// Bundle reads authority and every member in one statement snapshot.
func (repository *SessionQueries) Bundle(
	ctx context.Context,
	ref application.SessionRef,
	kind string,
) (application.BundleRecord, bool, error) {
	query := `
 SELECT session.credential_sha256,session.state,session.hard_expires_at_ms,file.logical_name,blob.value,
json_extract(blob.value, '$.sha256')
 FROM launch_sessions session
 LEFT JOIN launch_external_files file ON file.launch_session_id=session.id AND file.kind=?
 LEFT JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
 WHERE session.id=? ORDER BY file.logical_name`
	if ref.Preview {
		query = `
 SELECT session.credential_sha256,session.state,session.hard_expires_at_ms,file.logical_name,blob.value,
json_extract(blob.value, '$.sha256')
 FROM runtime_preview_sessions session
 LEFT JOIN runtime_preview_files file ON file.preview_session_id=session.id AND file.role=?
 LEFT JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
 WHERE session.id=? ORDER BY file.sort_order,file.logical_name`
	}
	rows, err := repository.executor.QueryContext(ctx, query, kind, ref.ID)
	if err != nil {
		return application.BundleRecord{}, false, fmt.Errorf("query launch bundle: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := application.BundleRecord{Files: []application.BundleFile{}}
	found := false
	for rows.Next() {
		var name, id, digest sql.NullString
		err := rows.Scan(
			&result.Session.CredentialHash,
			&result.Session.State,
			&result.Session.HardExpiresAtMS,
			&name, &id,
			&digest,
		)
		if err != nil {
			return application.BundleRecord{}, false, fmt.Errorf("scan launch bundle: %w", err)
		}
		found = true
		if name.Valid {
			result.Files = append(
				result.Files,
				application.BundleFile{FileRecord: id.String, LogicalName: name.String, SHA256: digest.String},
			)
		}
	}
	if err := rows.Err(); err != nil {
		return application.BundleRecord{}, false, fmt.Errorf("iterate launch bundle: %w", err)
	}
	return result, found, nil
}
