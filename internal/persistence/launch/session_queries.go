package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/launch"
)

type SessionQueries struct{ executor dbexec.Executor }

func NewSessionQueries(executor dbexec.Executor) *SessionQueries {
	return &SessionQueries{executor: executor}
}

func (repository *SessionQueries) Session(
	ctx context.Context,
	ref application.SessionRef,
) (application.SessionRecord, bool, error) {
	query := `SELECT credential_sha256,state,hard_expires_at_ms,provider_id,target_id,bundle_sha256,save_access
 FROM launch_sessions WHERE id=?`
	if ref.Preview {
		query = `SELECT credential_sha256,state,hard_expires_at_ms,provider_id,target_id,bundle_sha256,'NORMAL'
 FROM review_preview_sessions WHERE id=?`
	}
	return scanSession(repository.executor.QueryRowContext(ctx, query, ref.ID))
}

func (repository *SessionQueries) SaveSession(ctx context.Context, id string) (application.SessionRecord, bool, error) {
	return scanSession(repository.executor.QueryRowContext(ctx, `
 SELECT credential_sha256,state,hard_expires_at_ms,provider_id,target_id,bundle_sha256,save_access
 FROM launch_sessions WHERE id=?
 UNION ALL
 SELECT credential_sha256,state,hard_expires_at_ms,provider_id,target_id,bundle_sha256,'NORMAL'
 FROM review_preview_sessions WHERE id=?
 `, id, id))
}

func scanSession(row dbexec.Scanner) (application.SessionRecord, bool, error) {
	var result application.SessionRecord
	err := row.Scan(&result.CredentialHash, &result.State, &result.HardExpiresAtMS,
		&result.ProviderID, &result.TargetID, &result.BundleSHA256, &result.SaveAccess)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SessionRecord{}, false, nil
	}
	if err != nil {
		return application.SessionRecord{}, false, fmt.Errorf("scan launch session: %w", err)
	}
	return result, true, nil
}

func (repository *SessionQueries) MultiDisc(ctx context.Context, id string) (application.MultiDiscRecord, bool, error) {
	var result application.MultiDiscRecord
	session, dimensions := &result.Session, &result.Dimensions
	err := repository.executor.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.hard_expires_at_ms,platform.id,
 launch.target_id,launch.bundle_sha256,
 (SELECT count(*) FROM launch_external_files file WHERE file.launch_session_id=launch.id AND file.kind='DISC')
FROM launch_sessions launch
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN launch_content_files content ON content.launch_session_id=launch.id
WHERE launch.id=? AND content.format_version='RETROM_MULTIDISC_M3U_V1'
`, id).Scan(
		&session.CredentialHash, &session.State, &session.HardExpiresAtMS,
		&dimensions.PlatformKey, &dimensions.TargetKey, &dimensions.BundleDigest, &dimensions.DiscCount)
	if errors.Is(err, sql.ErrNoRows) {
		return application.MultiDiscRecord{}, false, nil
	}
	if err != nil {
		return application.MultiDiscRecord{}, false, fmt.Errorf("scan multidisc launch: %w", err)
	}
	return result, true, nil
}
