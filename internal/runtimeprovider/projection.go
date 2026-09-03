package runtimeprovider

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"

	"retrom/internal/cleanup"
	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
)

var (
	ErrProjectionInvalid            = errors.New("RUNTIME_PROVIDER_PROJECTION_INVALID")
	ErrProviderDowngrade            = errors.New("RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN")
	ErrProviderVersionRebuilt       = errors.New("RUNTIME_PROVIDER_VERSION_REBUILT")
	ErrProviderCompatibilityChanged = errors.New("RUNTIME_PROVIDER_COMPATIBILITY_CHANGED")
	ErrProviderTargetReferenced     = errors.New("RUNTIME_PROVIDER_TARGET_REFERENCED")
	ErrProviderCheckpointUnreadable = errors.New("RUNTIME_PROVIDER_CHECKPOINT_FORMAT_UNREADABLE")
	ErrCatalogDowngrade             = errors.New("RUNTIME_TARGET_CATALOG_DOWNGRADE_FORBIDDEN")
	ErrCatalogVersionRebuilt        = errors.New("RUNTIME_TARGET_CATALOG_VERSION_REBUILT")
)

// Projection is a fully validated, immutable startup candidate. Filesystem and
// network validation happen before this value is created; Reconcile performs
// only bounded database reads and one SQLite transaction.
type Projection struct {
	providers      []providerProjection
	bindings       []runtimecatalog.Binding
	catalogVersion int
	catalogSHA256  string
}

type providerProjection struct {
	active  runtimebundle.ActiveProvider
	source  string
	release *runtimebundle.ReleaseIdentity
	targets []targetProjection
}

type targetProjection struct {
	target             runtimebundle.Target
	capabilitiesJSON   string
	checkpointJSON     *string
	manifestFragment   string
	targetContractHash string
}

type currentProvider struct {
	version      string
	bundleSHA256 string
}

type currentTarget struct {
	gameCompatibilityLine    string
	netplayCompatibilityLine sql.NullString
}

func NewProjection(
	active runtimebundle.ActiveDescriptor,
	manifests map[string]runtimebundle.Manifest,
	catalog runtimecatalog.Catalog,
) (Projection, error) {
	if catalog.SchemaVersion != 1 || catalog.CatalogVersion < 1 || len(active.Providers) == 0 ||
		len(active.Providers) != len(manifests) {
		return Projection{}, ErrProjectionInvalid
	}
	targetExists := make(map[string]bool)
	providers := make([]providerProjection, 0, len(active.Providers))
	for _, provider := range active.Providers {
		manifest, exists := manifests[provider.ProviderID]
		if !exists {
			return Projection{}, ErrProjectionInvalid
		}
		projected, err := projectProvider(active, provider, manifest, targetExists)
		if err != nil {
			return Projection{}, err
		}
		providers = append(providers, projected)
	}
	if err := runtimecatalog.ValidateManifestBindings(catalog, func(providerID, targetID string) bool {
		return targetExists[providerID+"\x00"+targetID]
	}); err != nil {
		return Projection{}, projectionInvalid(err)
	}
	catalogContents, err := json.Marshal(catalog)
	if err != nil {
		return Projection{}, projectionInvalid(err)
	}
	return Projection{
		providers: providers, bindings: append([]runtimecatalog.Binding(nil), catalog.Bindings...),
		catalogVersion: catalog.CatalogVersion,
		catalogSHA256:  projectionDigest(catalogContents),
	}, nil
}

func projectProvider(
	active runtimebundle.ActiveDescriptor,
	provider runtimebundle.ActiveProvider,
	manifest runtimebundle.Manifest,
	targetExists map[string]bool,
) (providerProjection, error) {
	if manifest.ProviderID != provider.ProviderID || manifest.ProviderVersion != provider.ProviderVersion ||
		manifest.ProviderAPI != provider.ProviderAPI || manifest.ClientModulePath != provider.ClientModulePath ||
		len(manifest.Targets) != len(provider.Targets) {
		return providerProjection{}, ErrProjectionInvalid
	}
	byID := make(map[string]runtimebundle.ActiveTarget, len(provider.Targets))
	for _, target := range provider.Targets {
		byID[target.ID] = target
	}
	projected := providerProjection{active: provider, source: active.Source, release: active.Release}
	for _, target := range manifest.Targets {
		identity := provider.ProviderID + "\x00" + target.ID
		activeTarget, exists := byID[target.ID]
		if !exists || targetExists[identity] || !targetMatchesActiveProjection(target, activeTarget) {
			return providerProjection{}, ErrProjectionInvalid
		}
		projectedTarget, err := projectTarget(target)
		if err != nil {
			return providerProjection{}, err
		}
		projected.targets = append(projected.targets, projectedTarget)
		targetExists[identity] = true
	}
	return projected, nil
}

func targetMatchesActiveProjection(target runtimebundle.Target, active runtimebundle.ActiveTarget) bool {
	return active.ContractSHA256 == target.ContractSHA256 &&
		active.GameCompatibilityLine == target.GameCompatibilityLine &&
		equalOptionalString(active.NetplayCompatibilityLine, target.NetplayCompatibilityLine) &&
		equalCheckpoint(active.Checkpoint, target.Checkpoint)
}

func projectTarget(target runtimebundle.Target) (targetProjection, error) {
	capabilities, err := json.Marshal(target.Capabilities)
	if err != nil {
		return targetProjection{}, projectionInvalid(err)
	}
	var checkpointJSON *string
	if target.Checkpoint != nil {
		contents, marshalErr := json.Marshal(target.Checkpoint)
		if marshalErr != nil {
			return targetProjection{}, projectionInvalid(marshalErr)
		}
		value := string(contents)
		checkpointJSON = &value
	}
	fragment, err := json.Marshal(target)
	if err != nil {
		return targetProjection{}, projectionInvalid(err)
	}
	return targetProjection{
		target: target, capabilitiesJSON: string(capabilities), checkpointJSON: checkpointJSON,
		manifestFragment: string(fragment), targetContractHash: target.ContractSHA256,
	}, nil
}

func Reconcile(ctx context.Context, database *sql.DB, candidate Projection, now time.Time) error {
	if database == nil || candidate.catalogVersion < 1 || len(candidate.providers) == 0 || now.UnixMilli() < 0 {
		return ErrProjectionInvalid
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: %w", err)
	}
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			cleanup.Error("rollback runtime provider reconciliation", rollbackErr)
		}
	}()

	currentProviders, changedProviders, catalogChanged, err := prepareReconciliation(
		ctx, transaction, candidate,
	)
	if err != nil {
		return err
	}
	if len(changedProviders) == 0 && !catalogChanged {
		return commitProjection(transaction, "unchanged projection")
	}
	if err := applyChangedProjection(
		ctx,
		transaction,
		currentProviders,
		candidate,
		changedProviders,
		now.UnixMilli(),
	); err != nil {
		return err
	}
	return commitProjection(transaction, "projection")
}

func applyChangedProjection(
	ctx context.Context,
	transaction *sql.Tx,
	currentProviders map[string]currentProvider,
	candidate Projection,
	changedProviders []string,
	now int64,
) error {
	for _, providerID := range changedProviders {
		if err := terminateProviderSessions(ctx, transaction, providerID, now); err != nil {
			return err
		}
	}
	if err := writeProjection(ctx, transaction, currentProviders, candidate, now); err != nil {
		return err
	}
	return writeReconciliationAudit(ctx, transaction, candidate, changedProviders, now)
}

func commitProjection(transaction *sql.Tx, label string) error {
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("reconcile runtime providers: commit %s: %w", label, err)
	}
	return nil
}

func prepareReconciliation(
	ctx context.Context,
	transaction *sql.Tx,
	candidate Projection,
) (map[string]currentProvider, []string, bool, error) {
	currentProviders, err := loadCurrentProviders(ctx, transaction)
	if err != nil {
		return nil, nil, false, err
	}
	currentTargets, err := loadCurrentTargets(ctx, transaction)
	if err != nil {
		return nil, nil, false, err
	}
	changedProviders, err := validateProviderTransition(
		ctx, transaction, currentProviders, currentTargets, candidate,
	)
	if err != nil {
		return nil, nil, false, err
	}
	catalogChanged, err := validateCatalogTransition(ctx, transaction, candidate)
	if err != nil {
		return nil, nil, false, err
	}
	return currentProviders, changedProviders, catalogChanged, nil
}

func writeReconciliationAudit(
	ctx context.Context,
	transaction *sql.Tx,
	candidate Projection,
	changedProviders []string,
	now int64,
) error {
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: create audit id: %w", err)
	}
	diff, err := json.Marshal(map[string]any{
		"catalogSha256":  candidate.catalogSHA256,
		"catalogVersion": candidate.catalogVersion,
		"providers":      changedProviders,
	})
	if err != nil {
		return projectionInvalid(err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
  id,actor_kind,actor_label,action,resource_type,resource_id,diff_json,created_at_ms
) VALUES(?,'SYSTEM','runtime-provider-reconciliation','RUNTIME_PROVIDER_RECONCILED',
  'RUNTIME_PROVIDER_CATALOG','active',?,?)
`, auditID.String(), string(diff), now); err != nil {
		return fmt.Errorf("reconcile runtime providers: write audit: %w", err)
	}
	return nil
}

func loadCurrentProviders(ctx context.Context, transaction *sql.Tx) (map[string]currentProvider, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT provider_id,provider_version,bundle_sha256 FROM runtime_providers`)
	if err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: read providers: %w", err)
	}
	defer func() { cleanup.Error("close provider rows", rows.Close()) }()
	result := make(map[string]currentProvider)
	for rows.Next() {
		var providerID string
		var provider currentProvider
		if err := rows.Scan(&providerID, &provider.version, &provider.bundleSHA256); err != nil {
			return nil, fmt.Errorf("reconcile runtime providers: scan provider: %w", err)
		}
		result[providerID] = provider
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: providers: %w", err)
	}
	return result, nil
}

func loadCurrentTargets(ctx context.Context, transaction *sql.Tx) (map[string]currentTarget, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT provider_id,target_id,game_compatibility_line,netplay_compatibility_line FROM runtime_targets
`)
	if err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: read targets: %w", err)
	}
	defer func() { cleanup.Error("close target rows", rows.Close()) }()
	result := make(map[string]currentTarget)
	for rows.Next() {
		var providerID, targetID string
		var target currentTarget
		if err := rows.Scan(
			&providerID,
			&targetID,
			&target.gameCompatibilityLine,
			&target.netplayCompatibilityLine,
		); err != nil {
			return nil, fmt.Errorf("reconcile runtime providers: scan target: %w", err)
		}
		result[providerID+"\x00"+targetID] = target
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: targets: %w", err)
	}
	return result, nil
}

func validateProviderTransition(
	ctx context.Context,
	transaction *sql.Tx,
	currentProviders map[string]currentProvider,
	currentTargets map[string]currentTarget,
	candidate Projection,
) ([]string, error) {
	candidateProviders := make(map[string]providerProjection, len(candidate.providers))
	candidateTargets := make(map[string]targetProjection)
	changed := make([]string, 0, len(candidate.providers))
	for _, provider := range candidate.providers {
		providerID := provider.active.ProviderID
		candidateProviders[providerID] = provider
		current, exists := currentProviders[providerID]
		providerChanged, err := validateProviderVersion(provider.active, current, exists)
		if err != nil {
			return nil, err
		}
		if providerChanged {
			changed = append(changed, providerID)
		}
		for _, target := range provider.targets {
			identity := providerID + "\x00" + target.target.ID
			candidateTargets[identity] = target
			previous, targetExists := currentTargets[identity]
			if err := validateTargetTransition(
				ctx, transaction, providerID, target, previous, targetExists,
			); err != nil {
				return nil, err
			}
		}
	}
	removedProviders, err := validateRemovedProviders(ctx, transaction, currentProviders, candidateProviders)
	if err != nil {
		return nil, err
	}
	changed = append(changed, removedProviders...)
	for identity := range currentTargets {
		if _, exists := candidateTargets[identity]; exists {
			continue
		}
		providerID, targetID := splitIdentity(identity)
		if err := ensureTargetUnreferenced(ctx, transaction, providerID, targetID); err != nil {
			return nil, err
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func validateProviderVersion(
	candidate runtimebundle.ActiveProvider,
	current currentProvider,
	exists bool,
) (bool, error) {
	if !exists {
		return true, nil
	}
	comparison := semver.Compare("v"+candidate.ProviderVersion, "v"+current.version)
	if comparison < 0 {
		return false, fmt.Errorf("%w: %s", ErrProviderDowngrade, candidate.ProviderID)
	}
	if comparison == 0 && candidate.BundleSHA256 != current.bundleSHA256 {
		return false, fmt.Errorf("%w: %s", ErrProviderVersionRebuilt, candidate.ProviderID)
	}
	return comparison > 0 || candidate.BundleSHA256 != current.bundleSHA256, nil
}

func validateTargetTransition(
	ctx context.Context,
	transaction *sql.Tx,
	providerID string,
	target targetProjection,
	previous currentTarget,
	exists bool,
) error {
	if exists &&
		(previous.gameCompatibilityLine != target.target.GameCompatibilityLine ||
			!nullMatches(previous.netplayCompatibilityLine, target.target.NetplayCompatibilityLine)) {
		return fmt.Errorf("%w: %s/%s", ErrProviderCompatibilityChanged, providerID, target.target.ID)
	}
	return validateCheckpointFormats(ctx, transaction, providerID, target)
}

func validateRemovedProviders(
	ctx context.Context,
	transaction *sql.Tx,
	current map[string]currentProvider,
	candidate map[string]providerProjection,
) ([]string, error) {
	removed := make([]string, 0)
	for providerID := range current {
		if _, exists := candidate[providerID]; exists {
			continue
		}
		if err := ensureProviderUnreferenced(ctx, transaction, providerID); err != nil {
			return nil, err
		}
		removed = append(removed, providerID)
	}
	return removed, nil
}

func validateCatalogTransition(ctx context.Context, transaction *sql.Tx, candidate Projection) (bool, error) {
	var version int
	var digest string
	err := transaction.QueryRowContext(ctx, `
SELECT catalog_version,catalog_sha256 FROM runtime_catalog_state WHERE singleton=1
`).Scan(&version, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reconcile runtime providers: read catalog state: %w", err)
	}
	if candidate.catalogVersion < version {
		return false, ErrCatalogDowngrade
	}
	if candidate.catalogVersion == version && candidate.catalogSHA256 != digest {
		return false, ErrCatalogVersionRebuilt
	}
	return candidate.catalogVersion > version, nil
}

func validateCheckpointFormats(
	ctx context.Context,
	transaction *sql.Tx,
	providerID string,
	target targetProjection,
) error {
	formats := make(map[string]bool)
	if target.target.Checkpoint != nil {
		for _, format := range target.target.Checkpoint.ReadFormats {
			formats[format] = true
		}
	}
	queries := []string{
		`SELECT DISTINCT checkpoint_format FROM save_states WHERE provider_id=? AND target_id=? AND deleted_at_ms IS NULL`,
		`SELECT DISTINCT checkpoint.checkpoint_format
FROM rpgmaker_runtime_validation_checkpoints checkpoint
JOIN rpgmaker_runtime_validations validation ON validation.id=checkpoint.validation_id
WHERE validation.provider_id=? AND validation.target_id=?`,
	}
	for _, query := range queries {
		if err := validateCheckpointQuery(
			ctx, transaction, query, providerID, target.target.ID, formats,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateCheckpointQuery(
	ctx context.Context,
	transaction *sql.Tx,
	query, providerID, targetID string,
	formats map[string]bool,
) error {
	rows, err := transaction.QueryContext(ctx, query, providerID, targetID)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: read checkpoint formats: %w", err)
	}
	defer func() { cleanup.Error("close checkpoint formats", rows.Close()) }()
	for rows.Next() {
		var format string
		if err := rows.Scan(&format); err != nil {
			return fmt.Errorf("reconcile runtime providers: scan checkpoint format: %w", err)
		}
		if !formats[format] {
			return fmt.Errorf("%w: %s/%s %s", ErrProviderCheckpointUnreadable, providerID, targetID, format)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reconcile runtime providers: checkpoint formats: %w", err)
	}
	return nil
}

func ensureProviderUnreferenced(ctx context.Context, transaction *sql.Tx, providerID string) error {
	var targetID string
	err := transaction.QueryRowContext(ctx, `
SELECT target_id FROM runtime_targets WHERE provider_id=? ORDER BY target_id LIMIT 1
`, providerID).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: inspect provider targets: %w", err)
	}
	return ensureTargetUnreferenced(ctx, transaction, providerID, targetID)
}

func ensureTargetUnreferenced(ctx context.Context, transaction *sql.Tx, providerID, targetID string) error {
	references := []struct{ table, providerColumn, targetColumn string }{
		{"bios_requirements", "provider_id", "target_id"},
		{"dat_versions", "provider_id", "target_id"},
		{"server_bios_import_items", "provider_id", "target_id"},
		{"import_jobs", "provider_id", "target_id"},
		{"import_item_core_validations", "provider_id", "target_id"},
		{"rpgmaker_review_profiles", "provider_id", "target_id"},
		{"rpgmaker_runtime_validations", "provider_id", "target_id"},
		{"review_preview_sessions", "provider_id", "target_id"},
		{"review_runtime_screenshots", "provider_id", "target_id"},
		{"game_variant_revisions", "provider_id", "target_id"},
		{"launch_sessions", "provider_id", "target_id"},
		{"save_states", "provider_id", "target_id"},
		{"netplay_sessions", "provider_id", "target_id"},
		{"pegasus_import_collections", "target_provider_id", "target_id"},
		{"emulationstation_import_collections", "target_provider_id", "target_id"},
	}
	for _, reference := range references {
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=? AND %s=? LIMIT 1)",
			reference.table, reference.providerColumn, reference.targetColumn)
		var exists bool
		if err := transaction.QueryRowContext(ctx, query, providerID, targetID).Scan(&exists); err != nil {
			return fmt.Errorf("reconcile runtime providers: inspect %s: %w", reference.table, err)
		}
		if exists {
			return fmt.Errorf("%w: %s/%s", ErrProviderTargetReferenced, providerID, targetID)
		}
	}
	return nil
}

func terminateProviderSessions(ctx context.Context, transaction *sql.Tx, providerID string, now int64) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE provider_id=? AND state IN ('CREATED','ACTIVE')`, []any{now, now, providerID}},
		{`UPDATE review_preview_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE provider_id=? AND state IN ('CREATED','ACTIVE')`, []any{now, now, providerID}},
		{`UPDATE rpgmaker_runtime_validations SET state='EXPIRED',failure_code='RUNTIME_PROVIDER_UPGRADED',
updated_at_ms=? WHERE provider_id=? AND state NOT IN ('PASSED','FAILED','EXPIRED')`, []any{now, providerID}},
		{`UPDATE netplay_sessions SET state='FAILED',end_reason='SERVER_RESTARTED',
finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE provider_id=? AND state NOT IN ('FINISHED','FAILED')`, []any{now, now, providerID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("reconcile runtime providers: terminate sessions: %w", err)
		}
	}
	return nil
}

func writeProjection(
	ctx context.Context,
	transaction *sql.Tx,
	currentProviders map[string]currentProvider,
	candidate Projection,
	now int64,
) error {
	if err := clearHostBindings(ctx, transaction); err != nil {
		return err
	}
	candidateProviders, candidateTargets, err := writeProvidersAndTargets(ctx, transaction, candidate, now)
	if err != nil {
		return err
	}
	staleTargets, err := findStaleTargets(ctx, transaction, candidateTargets)
	if err != nil {
		return err
	}
	if err := removeStaleProjection(ctx, transaction, currentProviders, candidateProviders, staleTargets); err != nil {
		return err
	}
	if err := writeHostBindings(ctx, transaction, candidate.bindings); err != nil {
		return err
	}
	return writeCatalogState(ctx, transaction, candidate, now)
}

func clearHostBindings(ctx context.Context, transaction *sql.Tx) error {
	tables := []struct {
		name  string
		label string
	}{
		{"runtime_binding_content_kinds", "binding content kinds"},
		{"runtime_binding_platforms", "binding platforms"},
		{"runtime_target_bindings", "host bindings"},
	}
	for _, table := range tables {
		query := "DELETE FROM " + table.name
		if _, err := transaction.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("reconcile runtime providers: clear %s: %w", table.label, err)
		}
	}
	return nil
}

func writeProvidersAndTargets(
	ctx context.Context,
	transaction *sql.Tx,
	candidate Projection,
	now int64,
) (map[string]bool, map[string]bool, error) {
	candidateProviders := make(map[string]bool, len(candidate.providers))
	candidateTargets := make(map[string]bool)
	for _, provider := range candidate.providers {
		candidateProviders[provider.active.ProviderID] = true
		if err := writeProvider(ctx, transaction, provider, now); err != nil {
			return nil, nil, err
		}
		for _, target := range provider.targets {
			identity := provider.active.ProviderID + "\x00" + target.target.ID
			candidateTargets[identity] = true
			if err := writeTarget(ctx, transaction, provider.active.ProviderID, target); err != nil {
				return nil, nil, err
			}
		}
	}
	return candidateProviders, candidateTargets, nil
}

func writeProvider(
	ctx context.Context,
	transaction *sql.Tx,
	provider providerProjection,
	now int64,
) error {
	var repository, tag, commit any
	if provider.release != nil {
		repository, tag, commit = provider.release.Repository, provider.release.Tag, provider.release.Commit
	}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_providers(
  provider_id,provider_version,provider_api_version,bundle_sha256,manifest_sha256,module_sha256,
  source,release_repository,release_tag,release_commit,activated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(provider_id) DO UPDATE SET
  provider_version=excluded.provider_version,provider_api_version=excluded.provider_api_version,
  bundle_sha256=excluded.bundle_sha256,manifest_sha256=excluded.manifest_sha256,
  module_sha256=excluded.module_sha256,source=excluded.source,
  release_repository=excluded.release_repository,release_tag=excluded.release_tag,
  release_commit=excluded.release_commit,activated_at_ms=excluded.activated_at_ms
`, provider.active.ProviderID, provider.active.ProviderVersion, provider.active.ProviderAPI,
		provider.active.BundleSHA256, provider.active.ManifestSHA256, provider.active.ModuleSHA256,
		provider.source, repository, tag, commit, now)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write provider: %w", err)
	}
	return nil
}

func writeTarget(
	ctx context.Context,
	transaction *sql.Tx,
	providerID string,
	projected targetProjection,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_targets(
  provider_id,target_id,display_name,game_compatibility_line,netplay_compatibility_line,
  options_kind,capabilities_json,checkpoint_json,manifest_fragment_json,target_contract_sha256
) VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(provider_id,target_id) DO UPDATE SET
  display_name=excluded.display_name,game_compatibility_line=excluded.game_compatibility_line,
  netplay_compatibility_line=excluded.netplay_compatibility_line,options_kind=excluded.options_kind,
  capabilities_json=excluded.capabilities_json,checkpoint_json=excluded.checkpoint_json,
  manifest_fragment_json=excluded.manifest_fragment_json,target_contract_sha256=excluded.target_contract_sha256
`, providerID, projected.target.ID, projected.target.DisplayName,
		projected.target.GameCompatibilityLine, projected.target.NetplayCompatibilityLine,
		projected.target.OptionsKind, projected.capabilitiesJSON, projected.checkpointJSON,
		projected.manifestFragment, projected.targetContractHash)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write target: %w", err)
	}
	return nil
}

func findStaleTargets(
	ctx context.Context,
	transaction *sql.Tx,
	candidateTargets map[string]bool,
) ([][2]string, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT provider_id,target_id FROM runtime_targets`)
	if err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: enumerate stale targets: %w", err)
	}
	defer func() { cleanup.Error("close stale target rows", rows.Close()) }()
	var staleTargets [][2]string
	for rows.Next() {
		var providerID, targetID string
		if err := rows.Scan(&providerID, &targetID); err != nil {
			return nil, fmt.Errorf("reconcile runtime providers: scan stale target: %w", err)
		}
		if !candidateTargets[providerID+"\x00"+targetID] {
			staleTargets = append(staleTargets, [2]string{providerID, targetID})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: stale targets: %w", err)
	}
	return staleTargets, nil
}

func removeStaleProjection(
	ctx context.Context,
	transaction *sql.Tx,
	currentProviders map[string]currentProvider,
	candidateProviders map[string]bool,
	staleTargets [][2]string,
) error {
	for _, target := range staleTargets {
		_, err := transaction.ExecContext(
			ctx,
			`DELETE FROM runtime_targets WHERE provider_id=? AND target_id=?`,
			target[0],
			target[1],
		)
		if err != nil {
			return fmt.Errorf("reconcile runtime providers: remove target: %w", err)
		}
	}
	for providerID := range currentProviders {
		if candidateProviders[providerID] {
			continue
		}
		if _, err := transaction.ExecContext(
			ctx,
			`DELETE FROM runtime_providers WHERE provider_id=?`,
			providerID,
		); err != nil {
			return fmt.Errorf("reconcile runtime providers: remove provider: %w", err)
		}
	}
	return nil
}

func writeHostBindings(
	ctx context.Context,
	transaction *sql.Tx,
	bindings []runtimecatalog.Binding,
) error {
	for _, binding := range bindings {
		if err := writeHostBinding(ctx, transaction, binding); err != nil {
			return err
		}
	}
	return nil
}

func writeHostBinding(ctx context.Context, transaction *sql.Tx, binding runtimecatalog.Binding) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy,review_policy
) VALUES(?,?,?,?,?,?,?,?)
`, binding.ID, binding.CoreID, binding.ProviderID, binding.TargetID, binding.DetectorProfile,
		binding.DeliveryProfile, binding.LaunchPolicy, binding.ReviewPolicy)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write host binding: %w", err)
	}
	for _, platformID := range binding.PlatformIDs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_binding_platforms(binding_id,platform_id,core_id) VALUES(?,?,?)
`, binding.ID, platformID, binding.CoreID); err != nil {
			return fmt.Errorf("reconcile runtime providers: write binding platform: %w", err)
		}
	}
	for _, contentKind := range binding.AcceptedContentKinds {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_binding_content_kinds(binding_id,content_kind) VALUES(?,?)
`, binding.ID, contentKind); err != nil {
			return fmt.Errorf("reconcile runtime providers: write binding content kind: %w", err)
		}
	}
	return nil
}

func writeCatalogState(ctx context.Context, transaction *sql.Tx, candidate Projection, now int64) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_catalog_state(singleton,catalog_version,catalog_sha256,activated_at_ms)
VALUES(1,?,?,?)
ON CONFLICT(singleton) DO UPDATE SET catalog_version=excluded.catalog_version,
  catalog_sha256=excluded.catalog_sha256,activated_at_ms=excluded.activated_at_ms
`, candidate.catalogVersion, candidate.catalogSHA256, now)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write catalog state: %w", err)
	}
	return nil
}

func splitIdentity(identity string) (string, string) {
	for index := range identity {
		if identity[index] == 0 {
			return identity[:index], identity[index+1:]
		}
	}
	return identity, ""
}

func nullMatches(previous sql.NullString, next *string) bool {
	return previous.Valid == (next != nil) && (!previous.Valid || previous.String == *next)
}

func equalOptionalString(left, right *string) bool {
	return (left == nil) == (right == nil) && (left == nil || *left == *right)
}

func equalCheckpoint(left, right *runtimebundle.Checkpoint) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func projectionInvalid(err error) error {
	return fmt.Errorf("%w: %w", ErrProjectionInvalid, err)
}

func projectionDigest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
