package dependencies

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"

	"retrom/internal/arcadedat"
	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/datindex"
)

func (set *Set) Bootstrap(ctx context.Context, database *sql.DB, now time.Time) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin dependency bootstrap: %w", err)
	}
	defer cleanup.Rollback(transaction)
	preferredVersions := make(map[string]string)
	for _, versionName := range set.Order {
		for _, core := range set.Versions[versionName].Manifest.EmulatorJS.SelectedCores {
			preferredVersions[core.CoreID] = versionName
		}
	}
	for _, versionName := range set.Order {
		version := set.Versions[versionName]
		licenseComponents := make(map[string]struct {
			Repository, SourceCommit, Association string
		}, len(version.Manifest.Licenses.Components))
		for _, component := range version.Manifest.Licenses.Components {
			licenseComponents[component.ComponentID] = struct {
				Repository, SourceCommit, Association string
			}{component.Repository, component.SourceCommit, component.BinaryAssociationStatus}
		}
		selectedCoreIDs := make(map[string]struct{}, len(version.Manifest.EmulatorJS.SelectedCores))
		for index, core := range version.Manifest.EmulatorJS.SelectedCores {
			selectedCoreIDs[core.CoreID] = struct{}{}
			if err := bootstrapCore(
				ctx,
				transaction,
				versionName,
				version,
				preferredVersions[core.CoreID] == versionName,
				index,
				core,
				licenseComponents[core.SourceComponentID],
				now,
			); err != nil {
				return err
			}
		}
		if err := bootstrapStaticBIOS(ctx, transaction, versionName, selectedCoreIDs, now); err != nil {
			return err
		}
		for _, core := range version.Manifest.Cores {
			if core.DAT == nil {
				continue
			}
			if err := bootstrapDAT(
				ctx,
				transaction,
				versionName,
				core.CoreID,
				core.DAT.LocalPath,
				core.DAT.SHA256,
				core.ParseStats.MachineCount,
				core.ParseStats.ROMEntryCount,
				core.ParseStats.DiskEntryCount,
				core.ParseStats.BIOSSetCount,
				core.ParseStats.DefaultBIOSSetCount,
				core.ParseStats.ExplicitBIOSMachineCount,
				core.ParseStats.BaseDependencyTargetCount,
				core.ParseStats.UnresolvedCloneofCount+core.ParseStats.UnresolvedRomofCount,
				preferredVersions[core.CoreID] == versionName,
				now,
			); err != nil {
				return err
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit dependency bootstrap: %w", err)
	}
	return nil
}

// BootstrapCatalogs materializes the byte-verified built-in DAT indexes. It is
// separate from the lightweight seed operation so focused store tests can seed
// dictionaries without repeatedly parsing the large pinned production inputs.
//
// Contract branches stay contiguous for a single auditable decision.
func (set *Set) BootstrapCatalogs(ctx context.Context, database *sql.DB, now time.Time) error {
	bootstrap := catalogBootstrap{ctx: ctx, database: database, now: now}
	versionNames := make([]string, 0, len(set.Versions))
	for versionName := range set.Versions {
		versionNames = append(versionNames, versionName)
	}
	sort.Strings(versionNames)
	for _, versionName := range versionNames {
		version := set.Versions[versionName]
		for index, core := range version.Manifest.Cores {
			if core.DAT == nil {
				continue
			}
			bootstrap.runCore(versionName, version, index)
		}
	}
	return bootstrap.firstFailure
}

type catalogBootstrap struct {
	ctx          context.Context
	database     *sql.DB
	now          time.Time
	firstFailure error
}

type builtInDATState struct {
	id          string
	parseStatus string
	indexed     int64
}

type catalogStats struct {
	machineCount              int64
	romEntryCount             int64
	diskEntryCount            int64
	biosSetCount              int64
	defaultBIOSSetCount       int64
	explicitBIOSMachineCount  int64
	baseDependencyTargetCount int64
	unresolvedCloneofCount    int64
	unresolvedRomofCount      int64
}

func (bootstrap *catalogBootstrap) runCore(versionName string, version *Version, index int) {
	core := version.Manifest.Cores[index]
	expected := catalogStats{
		machineCount:              core.ParseStats.MachineCount,
		romEntryCount:             core.ParseStats.ROMEntryCount,
		diskEntryCount:            core.ParseStats.DiskEntryCount,
		biosSetCount:              core.ParseStats.BIOSSetCount,
		defaultBIOSSetCount:       core.ParseStats.DefaultBIOSSetCount,
		explicitBIOSMachineCount:  core.ParseStats.ExplicitBIOSMachineCount,
		baseDependencyTargetCount: core.ParseStats.BaseDependencyTargetCount,
		unresolvedCloneofCount:    core.ParseStats.UnresolvedCloneofCount,
		unresolvedRomofCount:      core.ParseStats.UnresolvedRomofCount,
	}
	state, err := bootstrap.findDAT(versionName, core.CoreID, core.DAT.SHA256)
	if err != nil {
		bootstrap.fail(fmt.Errorf("find built-in DAT index: %w", err))
		return
	}
	if state.parseStatus == "READY" && state.indexed == expected.machineCount {
		bootstrap.activateReady(state.id)
		return
	}
	if state.parseStatus == "FAILED" {
		bootstrap.fail(fmt.Errorf("%w: built-in DAT has retained failure evidence", ErrInvalid))
		return
	}
	jobID, err := ensureBuiltInDATJob(
		bootstrap.ctx, bootstrap.database, state.id, core.DAT.SHA256, "retrom-dat-v1", bootstrap.now,
	)
	if err != nil {
		bootstrap.fail(err)
		return
	}
	if err := claimBuiltInDATJob(bootstrap.ctx, bootstrap.database, state.id, jobID, bootstrap.now); err != nil {
		bootstrap.fail(err)
		return
	}
	catalog, err := bootstrap.loadCatalog(
		version, core.CoreID, core.DAT.LocalPath, expected, state.indexed, state.id, jobID,
	)
	if err != nil {
		bootstrap.fail(err)
		return
	}
	if err := publishBuiltInDATCatalog(
		bootstrap.ctx, bootstrap.database, state.id, jobID, state.indexed, expected.machineCount, catalog, bootstrap.now,
	); err != nil {
		failBuiltInDAT(
			bootstrap.ctx, bootstrap.database, state.id, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", bootstrap.now,
		)
		bootstrap.fail(err)
	}
}

func (bootstrap *catalogBootstrap) findDAT(versionName, coreID, datSHA string) (builtInDATState, error) {
	var state builtInDATState
	err := bootstrap.database.QueryRowContext(bootstrap.ctx, `
SELECT d.id,
d.parse_status,
(SELECT count(*)
FROM dat_machines m
WHERE m.dat_version_id=d.id)
FROM dat_versions d
JOIN core_artifacts a ON a.id=d.core_artifact_id
WHERE d.source='BUILTIN'
AND d.sha256=?
AND d.parser_version='retrom-dat-v1'
AND a.core_id=?
AND a.emulatorjs_version=?
`, datSHA, coreID, versionName).
		Scan(&state.id, &state.parseStatus, &state.indexed)
	if err != nil {
		return state, fmt.Errorf("query built-in DAT index: %w", err)
	}
	return state, nil
}

func (bootstrap *catalogBootstrap) activateReady(datID string) {
	transaction, err := bootstrap.database.BeginTx(bootstrap.ctx, nil)
	if err == nil {
		err = activateBuiltInDAT(bootstrap.ctx, transaction, datID, bootstrap.now)
	}
	if err == nil {
		err = transaction.Commit()
	} else if transaction != nil {
		cleanup.Rollback(transaction)
	}
	if err != nil {
		bootstrap.fail(fmt.Errorf("activate ready built-in DAT: %w", err))
	}
}

func (bootstrap *catalogBootstrap) loadCatalog(
	version *Version,
	coreID, datLocalPath string,
	expected catalogStats,
	indexed int64,
	datID, jobID string,
) (arcadedat.Catalog, error) {
	if indexed == expected.machineCount {
		return catalogFromStats(expected), nil
	}
	file, err := os.Open(filepath.Join(version.DATRoot, filepath.FromSlash(datLocalPath)))
	if err != nil {
		failBuiltInDAT(
			bootstrap.ctx, bootstrap.database, datID, jobID, "DEPENDENCY_DAT_BLOB_UNAVAILABLE", bootstrap.now,
		)
		return arcadedat.Catalog{}, fmt.Errorf("open built-in DAT: %w", err)
	}
	catalog, parseErr := arcadedat.ParseCatalog(bootstrap.ctx, file, coreID)
	cleanup.Error("close", file.Close())
	if parseErr == nil && statsMatch(catalog.Stats, expected) {
		return catalog, nil
	}
	failureCode := "DEPENDENCY_DAT_STATISTICS_MISMATCH"
	resultErr := fmt.Errorf("%w: built-in DAT statistics mismatch", ErrInvalid)
	if parseErr != nil {
		failureCode = "DEPENDENCY_DAT_PARSE_FAILED"
		resultErr = fmt.Errorf("parse built-in DAT: %w", parseErr)
	}
	failBuiltInDAT(bootstrap.ctx, bootstrap.database, datID, jobID, failureCode, bootstrap.now)
	return arcadedat.Catalog{}, resultErr
}

func (bootstrap *catalogBootstrap) fail(err error) {
	if bootstrap.firstFailure == nil {
		bootstrap.firstFailure = err
	}
}

func claimBuiltInDATJob(ctx context.Context, database *sql.DB, datID, jobID string, now time.Time) error {
	startedAtMS := now.UnixMilli()
	claimed, err := database.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=COALESCE(execution_started_at_ms,
?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,
?),
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='builtin-dat-indexer',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
		startedAtMS,
		startedAtMS+int64(30*time.Minute/time.Millisecond),
		startedAtMS+60_000,
		startedAtMS,
		startedAtMS,
		jobID,
	)
	if err != nil {
		return fmt.Errorf("claim built-in DAT job: %w", err)
	}
	if changed, _ := claimed.RowsAffected(); changed != 1 {
		return errDATJobNotClaimed
	}
	_, _ = database.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='PARSING',
version=version+1,
updated_at_ms=?
WHERE id=?
AND parse_status IN ('PENDING',
'PARSING')
`,
		startedAtMS,
		datID,
	)
	_, _ = database.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'STARTED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
1),
?)
`,
		jobID,
		datID,
		startedAtMS,
	)
	return nil
}

func publishBuiltInDATCatalog(
	ctx context.Context,
	database *sql.DB,
	datID, jobID string,
	indexed, expectedMachineCount int64,
	catalog arcadedat.Catalog,
	now time.Time,
) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin built-in DAT publication: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if indexed != expectedMachineCount {
		if err := datindex.Replace(ctx, transaction, datID, catalog); err != nil {
			return fmt.Errorf("write built-in DAT index: %w", err)
		}
	}
	stats := catalog.Stats
	finishedAtMS := now.UnixMilli()
	var artifactID string
	if err := transaction.QueryRowContext(ctx, `
SELECT core_artifact_id
FROM dat_versions
WHERE id=?
`, datID).Scan(&artifactID); err != nil {
		return fmt.Errorf("find built-in DAT artifact: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='READY',
is_active=0,
machine_count=?,
rom_entry_count=?,
disk_entry_count=?,
bios_set_count=?,
default_bios_set_count=?,
explicit_bios_machine_count=?,
base_dependency_target_count=?,
unresolved_relation_count=?,
parsed_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`,
		stats.MachineCount,
		stats.ROMEntryCount,
		stats.DiskEntryCount,
		stats.BIOSSetCount,
		stats.DefaultBIOSSetCount,
		stats.ExplicitBIOSMachineCount,
		stats.BaseDependencyTargetCount,
		stats.UnresolvedCloneofTargetCount+stats.UnresolvedRomofTargetCount,
		finishedAtMS,
		finishedAtMS,
		datID,
	)
	if err != nil {
		return fmt.Errorf("publish built-in DAT index: %w", err)
	}
	if err := activateBuiltInDAT(ctx, transaction, datID, now); err != nil {
		return fmt.Errorf("publish built-in DAT requirements: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
heartbeat_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
`, finishedAtMS, finishedAtMS, finishedAtMS, jobID); err != nil {
		return fmt.Errorf("finish built-in DAT job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'SUCCEEDED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
1,
'machineCount',
?),
?)
`, jobID, datID, stats.MachineCount, finishedAtMS); err != nil {
		return fmt.Errorf("record built-in DAT success: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit built-in DAT index: %w", err)
	}
	return nil
}

func catalogFromStats(stats catalogStats) arcadedat.Catalog {
	return arcadedat.Catalog{Stats: arcadedat.Stats{
		MachineCount:                 int(stats.machineCount),
		ROMEntryCount:                int(stats.romEntryCount),
		DiskEntryCount:               int(stats.diskEntryCount),
		BIOSSetCount:                 int(stats.biosSetCount),
		DefaultBIOSSetCount:          int(stats.defaultBIOSSetCount),
		ExplicitBIOSMachineCount:     int(stats.explicitBIOSMachineCount),
		BaseDependencyTargetCount:    int(stats.baseDependencyTargetCount),
		UnresolvedCloneofTargetCount: int(stats.unresolvedCloneofCount),
		UnresolvedRomofTargetCount:   int(stats.unresolvedRomofCount),
	}}
}

func activateBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	datID string,
	now time.Time,
) error {
	var artifactID, source, parseStatus string
	var artifactEnabled, alreadyActive int
	if err := transaction.QueryRowContext(ctx, `
SELECT d.core_artifact_id,a.enabled,d.source,d.parse_status,d.is_active
FROM dat_versions d
JOIN core_artifacts a ON a.id=d.core_artifact_id
WHERE d.id=?
`, datID).Scan(&artifactID, &artifactEnabled, &source, &parseStatus, &alreadyActive); err != nil {
		return fmt.Errorf("inspect selected built-in DAT: %w", err)
	}
	if artifactEnabled == 0 {
		return nil
	}
	if source != "BUILTIN" || parseStatus != "READY" {
		return fmt.Errorf("%w: selected DAT is not a ready built-in version", ErrInvalid)
	}
	if alreadyActive == 1 {
		return nil
	}
	deactivated, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE core_artifact_id=? AND source='BUILTIN' AND is_active=1 AND id<>?
`, now.UnixMilli(), artifactID, datID)
	if err != nil {
		return fmt.Errorf("deactivate superseded built-in DAT: %w", err)
	}
	if changed, _ := deactivated.RowsAffected(); changed > 0 {
		if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, now.UnixMilli(), artifactID); err != nil {
			return fmt.Errorf("advance artifact DAT selection: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=1,activated_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND source='BUILTIN' AND parse_status='READY' AND is_active=0
`, now.UnixMilli(), now.UnixMilli(), datID); err != nil {
		return fmt.Errorf("activate selected built-in DAT: %w", err)
	}
	if err := datindex.SyncRequirements(ctx, transaction, datID, now); err != nil {
		return fmt.Errorf("sync selected built-in DAT requirements: %w", err)
	}
	auditID, _ := uuid.NewV7()
	actor := authn.ActorFromContext(ctx, "release-setup")
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,created_at_ms)
VALUES(?,?,?,?,'BUILTIN_DAT_ACTIVATED','DAT_VERSION',?,
'{"active":false}','{"active":true}',json_object('source','release-manifest'),?)
`, auditID.String(), actor.Kind, actor.UserID, actor.Label, datID, now.UnixMilli()); err != nil {
		return fmt.Errorf("audit selected built-in DAT activation: %w", err)
	}
	return nil
}

func ensureBuiltInDATJob(
	ctx context.Context,
	database *sql.DB,
	datID, datSHA, parserVersion string,
	now time.Time,
) (string, error) {
	canonical, _ := json.Marshal(map[string]string{"datVersionId": datID, "parserVersion": parserVersion})
	digest := sha256.New()
	_, _ = digest.Write([]byte("retrom-job-dedupe-v1\x00DAT_PARSE\x00"))
	_, _ = digest.Write(canonical)
	dedupeKey := hex.EncodeToString(digest.Sum(nil))
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var jobID, state string
	err = transaction.QueryRowContext(ctx, `
SELECT id,
state
FROM jobs
WHERE kind='DAT_PARSE'
AND dedupe_key=?
`, dedupeKey).
		Scan(&jobID, &state)
	if err == nil {
		return reuseBuiltInDATJob(ctx, transaction, jobID, state, now)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	return createBuiltInDATJob(
		ctx, transaction, datID, datSHA, parserVersion, dedupeKey, now,
	)
}

func reuseBuiltInDATJob(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, state string,
	now time.Time,
) (string, error) {
	if state == "FAILED" || state == "CANCELLED" {
		return "", errDATParseFailed
	}
	if state == "RUNNING" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',
execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,
leased_until_ms=NULL,
heartbeat_at_ms=NULL,
worker_id=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now.UnixMilli(), jobID); err != nil {
			return "", fmt.Errorf("dependencies/dependencies: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	return jobID, nil
}

func createBuiltInDATJob(
	ctx context.Context,
	transaction *sql.Tx,
	datID, datSHA, parserVersion, dedupeKey string,
	now time.Time,
) (string, error) {
	generated, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	jobID := generated.String()
	var datVersion int64
	var baseID sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT version,
(SELECT id
FROM dat_versions active
WHERE active.core_artifact_id=target.core_artifact_id
AND active.is_active=1)
FROM dat_versions target
WHERE target.id=?
`, datID).Scan(&datVersion, &baseID); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "DAT_PARSE",
		"scope":         map[string]any{"type": "DAT_VERSION", "id": datID},
		"executionId":   executionID.String(),
		"inputs": map[string]any{
			"datVersion":       datVersion,
			"datSha256":        datSHA,
			"parserVersion":    parserVersion,
			"baseDatVersionId": nullableString(baseID),
		},
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	payload := `{"schemaVersion":1,"inputExecutionNo":1}`
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'DAT_VERSION',
?,
'DAT_PARSE',
?,
1,
?,
0,
'QUEUED',
0,
2,
?,
?,
?)
`, jobID, datID, dedupeKey, payload, now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
1,
?,
?,
?)
`, jobID, string(inputJSON), hex.EncodeToString(inputDigest[:]), now.UnixMilli()); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'QUEUED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
0),
?)
`, jobID, datID, now.UnixMilli()); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	return jobID, nil
}

func failBuiltInDAT(ctx context.Context, database *sql.DB, datID, jobID, code string, now time.Time) {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	_, _ = transaction.ExecContext(
		ctx,
		`
UPDATE dat_versions
SET parse_status='FAILED',
is_active=0,
machine_count=NULL,
rom_entry_count=NULL,
disk_entry_count=NULL,
bios_set_count=NULL,
default_bios_set_count=NULL,
explicit_bios_machine_count=NULL,
base_dependency_target_count=NULL,
unresolved_relation_count=NULL,
parsed_at_ms=NULL,
updated_at_ms=?,
version=version+1
WHERE id=?
`,
		now.UnixMilli(),
		datID,
	)
	_, _ = transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=0,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
`,
		code,
		now.UnixMilli(),
		now.UnixMilli(),
		jobID,
	)
	_, _ = transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'FAILED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
1,
'errorCode',
?,
'errorRetryable',
false),
?)
`,
		jobID,
		datID,
		code,
		now.UnixMilli(),
	)
	_ = transaction.Commit()
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func statsMatch(actual arcadedat.Stats, expected catalogStats) bool {
	return int64(actual.MachineCount) == expected.machineCount && int64(actual.ROMEntryCount) == expected.romEntryCount &&
		int64(actual.DiskEntryCount) == expected.diskEntryCount &&
		int64(actual.BIOSSetCount) == expected.biosSetCount &&
		int64(actual.DefaultBIOSSetCount) == expected.defaultBIOSSetCount &&
		int64(actual.ExplicitBIOSMachineCount) == expected.explicitBIOSMachineCount &&
		int64(actual.BaseDependencyTargetCount) == expected.baseDependencyTargetCount &&
		int64(actual.UnresolvedCloneofTargetCount) == expected.unresolvedCloneofCount &&
		int64(actual.UnresolvedRomofTargetCount) == expected.unresolvedRomofCount
}
