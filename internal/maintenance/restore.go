package maintenance

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	// Register the modernc SQLite driver used by openDatabase.
	_ "modernc.org/sqlite"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
)

func Restore(ctx context.Context, configuration config.Maintenance, input, output string) (Manifest, error) {
	if !filepath.IsAbs(input) || !filepath.IsAbs(output) || filepath.Clean(input) != input ||
		filepath.Clean(output) != output ||
		exists(output) {
		return Manifest{}, ErrInvalidBundle
	}
	manifest, err := validateBundle(input)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateRestoreDependencies(configuration, manifest); err != nil {
		return Manifest{}, err
	}
	staging, err := createStaging(output)
	if err != nil {
		return Manifest{}, err
	}
	if err := copyRestoreFiles(input, staging, manifest); err != nil {
		return Manifest{}, err
	}
	if err := validateAndFenceRestore(ctx, staging); err != nil {
		return Manifest{}, err
	}
	if err := publishRestore(staging, output); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateRestoreDependencies(configuration config.Maintenance, manifest Manifest) error {
	if strings.Join(configuration.DependencyVersions, ",") != strings.Join(manifest.DependencyVersions, ",") ||
		configuration.ActiveEJSVersion != manifest.ActiveEmulatorjsVersion {
		return ErrDependencyMismatch
	}
	if _, err := dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	); err != nil {
		return ErrDependencyMismatch
	}
	for _, evidence := range manifest.DependencyManifests {
		manifestRoot := filepath.Join(configuration.DependencyRoot, "dat", "emulatorjs", evidence.Version)
		for _, value := range []struct{ source, expected string }{
			{filepath.Join(manifestRoot, "manifest.json"), evidence.ManifestSHA256},
			{filepath.Join(manifestRoot, "SHA256SUMS"), evidence.SHA256SumsSHA256},
		} {
			if digest, _, err := digestRegular(value.source); err != nil || digest != value.expected {
				return ErrDependencyMismatch
			}
		}
	}
	return nil
}

func copyRestoreFiles(input, staging string, manifest Manifest) error {
	for _, entry := range manifest.Files {
		if entry.Kind == "DEPENDENCY_MANIFEST" || entry.Kind == "DEPENDENCY_SHA256SUMS" {
			continue
		}
		if _, err := copyVerified(
			filepath.Join(input, filepath.FromSlash(entry.Path)),
			filepath.Join(staging, filepath.FromSlash(entry.Path)),
			entry.Path,
			entry.Kind,
			entry.SHA256,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateAndFenceRestore(ctx context.Context, staging string) error {
	database, err := openDatabase(ctx, filepath.Join(staging, "retrom.db"))
	if err != nil {
		return err
	}
	if err := checkDatabase(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return err
	}
	if err := blobregistry.ValidateSchema(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := validateRestoredFiles(ctx, database, staging); err != nil {
		cleanup.Error("close", database.Close())
		return err
	}
	if err := applyRestoreSecurityFence(ctx, database, time.Now().UTC()); err != nil {
		cleanup.Error("close", database.Close())
		return err
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	return nil
}

func publishRestore(staging, output string) error {
	if err := syncTree(staging); err != nil {
		return err
	}
	if err := os.Rename(staging, output); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return err
	}
	return nil
}

func fenceRestoredReviewBulk(ctx context.Context, transaction *sql.Tx, nowMS int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approval_items SET state='CANCELLED',outcome_code='RESTORE_INTERRUPTED',
outcome_details_json=json_object('schemaVersion',1,'code','RESTORE_INTERRUPTED'),completed_at_ms=?
WHERE bulk_approval_id IN (
  SELECT id FROM review_bulk_approvals WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
) AND state IN ('PENDING','RUNNING')
`, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_bulk_approvals SET state='FAILED',last_error_code='RESTORE_INTERRUPTED',
processed_count=candidate_count,
published_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='PUBLISHED'),
skipped_duplicate_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='SKIPPED_DUPLICATE'),
skipped_changed_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='SKIPPED_CHANGED'),
skipped_not_ready_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='SKIPPED_NOT_READY'),
failed_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='FAILED_FINAL'),
cancelled_count=(SELECT count(*) FROM review_bulk_approval_items item
  WHERE item.bulk_approval_id=review_bulk_approvals.id AND item.state='CANCELLED'),
cancel_requested_at_ms=NULL,cancel_reason=NULL,completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk approvals: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='RESTORE_INTERRUPTED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind='REVIEW_BULK_APPROVE' AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored review bulk jobs: %w", err)
	}
	return nil
}

// Restore revocations and external-source task fencing are one atomic security boundary.
func applyRestoreSecurityFence(ctx context.Context, database *sql.DB, now time.Time) error {
	nowMS := now.UnixMilli()
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: begin restore security fence: %w", err)
	}
	defer cleanup.Rollback(transaction)
	sessions, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='RESTORE' WHERE revoked_at_ms IS NULL
`, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored sessions: %w", err)
	}
	links, err := transaction.ExecContext(ctx, `
UPDATE account_links SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored account links: %w", err)
	}
	launches, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE state IN ('CREATED','ACTIVE')
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored launches: %w", err)
	}
	serverJobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind='SERVER_BIOS_IMPORT' AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored server import jobs: %w", err)
	}
	pegasusJobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT') AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored Pegasus jobs: %w", err)
	}
	if err := fenceRestoredReviewBulk(ctx, transaction, nowMS); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='COMMIT_FAILED',outcome_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
completed_at_ms=?,updated_at_ms=? WHERE server_import_id IN (
  SELECT id FROM server_imports WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
) AND state IN ('PENDING','EVALUATING')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored server import items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='FAILED',last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
cancel_requested_at_ms=NULL,cancel_reason=NULL,
imported_matched_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_MATCHED'),
imported_warning_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_WARNING'),
imported_missing_entry_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_MISSING_ENTRY'),
not_found_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='NOT_FOUND'),
skipped_existing_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='SKIPPED_EXISTING'),
skipped_not_better_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='SKIPPED_NOT_BETTER'),
same_bytes_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='ALREADY_SAME_BYTES'),
failed_item_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id
  AND item.state IN ('SOURCE_CHANGED','CATALOG_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM server_bios_import_items item
  WHERE item.server_import_id=server_imports.id AND item.state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored server imports: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items SET execution_state='COMMIT_FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,completed_at_ms=?,updated_at_ms=? WHERE import_id IN (
  SELECT id FROM pegasus_imports WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
) AND execution_state IN ('PENDING','COPYING','VALIDATING','PUBLISHING')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored Pegasus items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports SET state='FAILED',phase=NULL,last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,cancel_reason=NULL,
published_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='PUBLISHED'),
existing_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='SKIPPED_EXISTING'),
blocked_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id
  AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')),
failed_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id
  AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored Pegasus imports: %w", err)
	}
	sessionCount, _ := sessions.RowsAffected()
	linkCount, _ := links.RowsAffected()
	launchCount, _ := launches.RowsAffected()
	serverJobCount, _ := serverJobs.RowsAffected()
	pegasusJobCount, _ := pegasusJobs.RowsAffected()
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("maintenance/bundle: create restore audit ID: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'SYSTEM',NULL,'restore-security-fence','RESTORE_SECURITY_FENCE','INSTANCE','instance',
NULL,json_object('revokedSessionCount',?,'revokedAccountLinkCount',?,
'revokedLaunchCount',?,'failedServerImportCount',?,'failedPegasusJobCount',?),
'{}',NULL,?)
`, auditID.String(), sessionCount, linkCount, launchCount, serverJobCount, pegasusJobCount, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: audit restore security fence: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("maintenance/bundle: commit restore security fence: %w", err)
	}
	return nil
}
