package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
)

type validationInputs struct {
	GameVariantID         string `json:"gameVariantId"`
	GameContentRevisionID string `json:"gameContentRevisionId"`
	CoreArtifactID        string `json:"coreArtifactId"`
	DATVersionID          any    `json:"datVersionId"`
	ValidationInputDigest string `json:"validationInputDigest"`
	BIOSDependencyDigest  string `json:"biosDependencyDigest"`
}

type validationSnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Scope         validationScope  `json:"scope"`
	ExecutionID   string           `json:"executionId"`
	Inputs        validationInputs `json:"inputs"`
}

type validationScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func validationDedupeKey(variantID, digest string) string {
	canonical, _ := json.Marshal(map[string]string{"gameVariantId": variantID, "validationInputDigest": digest})
	value := sha256.New()
	_, _ = value.Write([]byte("retrom-job-dedupe-v1\x00VARIANT_REVALIDATE\x00"))
	_, _ = value.Write(canonical)
	return hex.EncodeToString(value.Sum(nil))
}

type variantArcadeBIOSRow struct {
	logicalName         string
	dependencyState     string
	requirementID       sql.NullString
	requirementVersion  sql.NullInt64
	catalogDigest       sql.NullString
	requirementMode     sql.NullString
	condition           sql.NullString
	deliveryKind        sql.NullString
	emulatorPath        sql.NullString
	optionsJSON         sql.NullString
	installationID      sql.NullString
	installationVersion sql.NullInt64
	blobID              sql.NullString
	installationStatus  sql.NullString
}

func scanVariantArcadeBIOSRow(rows *sql.Rows) (variantArcadeBIOSRow, error) {
	var row variantArcadeBIOSRow
	if err := rows.Scan(
		&row.logicalName,
		&row.dependencyState,
		&row.requirementID,
		&row.requirementVersion,
		&row.catalogDigest,
		&row.requirementMode,
		&row.condition,
		&row.deliveryKind,
		&row.emulatorPath,
		&row.optionsJSON,
		&row.installationID,
		&row.installationVersion,
		&row.blobID,
		&row.installationStatus,
	); err != nil {
		return variantArcadeBIOSRow{}, fmt.Errorf("scan Arcade BIOS dependency: %w", err)
	}
	return row, nil
}

func variantArcadeBIOSDependency(
	row variantArcadeBIOSRow,
) (corevalidation.BIOSDependency, bool, bool, error) {
	if row.dependencyState == "SATISFIED_BY_CONTENT" {
		return corevalidation.BIOSDependency{}, false, true, nil
	}
	if !row.requirementID.Valid || !row.requirementVersion.Valid || !row.catalogDigest.Valid ||
		!row.requirementMode.Valid || !row.deliveryKind.Valid {
		return corevalidation.BIOSDependency{}, false, false, nil
	}
	dependency := corevalidation.BIOSDependency{
		BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{
			RequirementID: row.requirementID.String, RequirementVersion: row.requirementVersion.Int64,
			CatalogDigest: row.catalogDigest.String, LogicalName: row.logicalName,
			RequirementMode: row.requirementMode.String, DeliveryKind: row.deliveryKind.String,
			ConditionCode: nullStringPointer(row.condition), EmulatorPath: nullStringPointer(row.emulatorPath),
		},
		ActivationOptions:   make(map[string]string),
		InstallationID:      nullStringPointer(row.installationID),
		InstallationVersion: nullInt64Pointer(row.installationVersion),
		BlobID:              nullStringPointer(row.blobID),
		InstallationStatus:  nullStringPointer(row.installationStatus),
	}
	if row.optionsJSON.Valid {
		if err := json.Unmarshal([]byte(row.optionsJSON.String), &dependency.ActivationOptions); err != nil {
			return corevalidation.BIOSDependency{}, false, false, corevalidation.ErrInvalidSnapshot
		}
	}
	validInstallation := row.blobID.Valid && row.installationStatus.Valid &&
		(row.installationStatus.String == "MATCHED" || row.installationStatus.String == "HASH_WARNING")
	return dependency, true, validInstallation, nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func (service *Service) resolveVariantBIOS(
	ctx context.Context,
	database corevalidation.Queryer,
	variantID, contentID, artifactID, contentLogicalName string,
	datID sql.NullString,
) (corevalidation.Snapshot, string, string, error) {
	snapshot, status, code, err := corevalidation.ResolveBIOS(ctx, database, artifactID, contentLogicalName)
	if err != nil {
		return snapshot, status, code, fmt.Errorf("launch validation static BIOS: %w", err)
	}
	if !datID.Valid {
		return snapshot, status, code, nil
	}
	rows, err := database.QueryContext(ctx, `
SELECT dependency.logical_archive,
dependency.state,
requirement.id,
requirement.version,
requirement.catalog_digest,
requirement.requirement_mode,
requirement.condition_code,
requirement.delivery_kind,
requirement.emulator_path,
requirement.activation_options_json,
installation.id,
installation.version,
installation.blob_id,
installation.status
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
AND revision.game_content_revision_id=?
AND revision.dat_version_id IS ?
JOIN variant_dependencies dependency ON dependency.game_variant_revision_id=revision.id
AND dependency.kind='BIOS_OR_BASE'
LEFT JOIN bios_requirements requirement ON requirement.core_artifact_id=?
AND requirement.source_kind='DAT_MACHINE'
AND requirement.source_version=?
AND requirement.logical_name=dependency.logical_archive
AND requirement.enabled=1
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
AND installation.is_active=1
AND installation.validated_requirement_version=requirement.version
WHERE variant.id=?
ORDER BY dependency.logical_archive
`, contentID, nullableSQL(datID), artifactID, datID.String, variantID)
	if err != nil {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
			fmt.Errorf("launch validation Arcade BIOS: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		row, err := scanVariantArcadeBIOSRow(rows)
		if err != nil {
			return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
				fmt.Errorf("launch validation Arcade BIOS: %w", err)
		}
		dependency, include, validInstallation, err := variantArcadeBIOSDependency(row)
		if err != nil {
			return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
				fmt.Errorf("launch validation Arcade BIOS dependency: %w", err)
		}
		if !validInstallation {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		if include {
			snapshot.BIOS = append(snapshot.BIOS, dependency)
		}
	}
	if err := rows.Err(); err != nil {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
			fmt.Errorf("launch validation Arcade BIOS: %w", err)
	}
	return snapshot, status, code, nil
}

func (service *Service) validationDigests(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, contentID, contentLogicalName, contentKind, artifactID string,
	datID sql.NullString,
) (string, string, error) {
	biosSnapshot, _, _, err := service.resolveVariantBIOS(
		ctx, transaction, variantID, contentID, artifactID, contentLogicalName, datID,
	)
	if err != nil {
		return "", "", ErrBlocked
	}
	if contentKind == corevalidation.MultiDiscContentKind {
		digest, biosDigest, _, digestErr := service.multiDiscRevalidationInputs(
			ctx, variantID, contentID, artifactID, datID, biosSnapshot,
		)
		return digest, biosDigest, digestErr
	}
	digest, err := corevalidation.ValidationInputDigest(artifactID, contentID, datID, biosSnapshot)
	if err != nil {
		return "", "", fmt.Errorf("launch validation digest: %w", err)
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return "", "", fmt.Errorf("launch validation snapshot: %w", err)
	}
	biosSnapshotDigest := sha256.Sum256(biosSnapshotJSON)
	return digest, hex.EncodeToString(biosSnapshotDigest[:]), nil
}

func (service *Service) currentValidationEvidence(
	ctx context.Context,
	variantID, contentID, contentLogicalName, contentKind, artifactID string,
	datID sql.NullString,
) (string, string, corevalidation.Snapshot, string, string, error) {
	biosSnapshot, biosStatus, biosCode, err := service.resolveVariantBIOS(
		ctx, service.database, variantID, contentID, artifactID, contentLogicalName, datID,
	)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("launch validation BIOS: %w", err)
	}
	if contentKind == corevalidation.MultiDiscContentKind {
		digest, biosDigest, snapshot, digestErr := service.multiDiscRevalidationInputs(
			ctx, variantID, contentID, artifactID, datID, biosSnapshot,
		)
		return digest, biosDigest, snapshot, biosStatus, biosCode, digestErr
	}
	digest, err := corevalidation.ValidationInputDigest(artifactID, contentID, datID, biosSnapshot)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("launch validation digest: %w", err)
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("launch validation snapshot: %w", err)
	}
	biosDigest := sha256.Sum256(biosSnapshotJSON)
	return digest, hex.EncodeToString(biosDigest[:]), biosSnapshot, biosStatus, biosCode, nil
}

type arcadeSnapshotIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	Machine       string `json:"machine"`
	DATVersionID  string `json:"datVersionId"`
}

type legacyArcadeClosureNode struct {
	Machine    string  `json:"machine"`
	Kind       string  `json:"kind"`
	RequiredBy *string `json:"requiredBy"`
	Depth      int     `json:"depth"`
}

type projectedArcadeDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type projectedArcadeSnapshot struct {
	SchemaVersion     int                         `json:"schemaVersion"`
	Machine           string                      `json:"machine"`
	DATVersionID      string                      `json:"datVersionId"`
	Closure           []legacyArcadeClosureNode   `json:"closure"`
	Dependencies      []projectedArcadeDependency `json:"dependencies"`
	MissingEntries    []string                    `json:"missingEntries"`
	MismatchedEntries []string                    `json:"mismatchedEntries"`
	Warnings          []string                    `json:"warnings"`
}

func validateLockedArcadeSnapshot(raw, contentLogicalName, datID string) error {
	var identity arcadeSnapshotIdentity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return corevalidation.ErrInvalidSnapshot
	}
	machine := strings.TrimSuffix(filepath.Base(contentLogicalName), filepath.Ext(contentLogicalName))
	if identity.SchemaVersion != 2 || identity.Machine != machine || identity.DATVersionID != datID {
		return corevalidation.ErrInvalidSnapshot
	}
	if _, err := corevalidation.ParseRuntimeBIOSDependencies(raw); err != nil {
		return fmt.Errorf("parse Arcade runtime dependency snapshot: %w", err)
	}
	return nil
}

func (service *Service) lockedArcadeDependencySnapshot(
	ctx context.Context,
	variantID, contentID, contentLogicalName, datID string,
) (string, error) {
	var revisionID, raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT revision.id,revision.dependency_snapshot_json
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE variant.id=?
AND revision.game_content_revision_id=?
AND revision.dat_version_id=?
`, variantID, contentID, datID).Scan(&revisionID, &raw); err != nil {
		return "", fmt.Errorf("load locked Arcade dependency snapshot: %w", err)
	}
	if err := validateLockedArcadeSnapshot(raw, contentLogicalName, datID); err == nil {
		return raw, nil
	}
	if _, err := corevalidation.ParseSnapshot(raw); err != nil {
		return "", fmt.Errorf("validate locked Arcade dependency snapshot: %w", err)
	}
	projected, err := service.projectLegacyArcadeSnapshot(ctx, revisionID, contentLogicalName, datID)
	if err != nil {
		return "", fmt.Errorf("project legacy Arcade dependency snapshot: %w", err)
	}
	return projected, nil
}

func (service *Service) projectLegacyArcadeSnapshot(
	ctx context.Context,
	revisionID, contentLogicalName, datID string,
) (string, error) {
	machine := strings.TrimSuffix(filepath.Base(contentLogicalName), filepath.Ext(contentLogicalName))
	closure, err := service.arcadeDependencyClosure(ctx, datID, machine)
	if err != nil {
		return "", err
	}
	dependencies, err := service.legacyArcadeDependencies(ctx, revisionID, datID, closure)
	if err != nil {
		return "", err
	}
	warnings := make([]string, 0)
	for _, dependency := range dependencies {
		if dependency.State == "HASH_WARNING" {
			warnings = append(warnings, dependency.Machine+".zip:HASH_WARNING")
		}
	}
	sort.Strings(warnings)
	snapshot := projectedArcadeSnapshot{
		SchemaVersion: 2, Machine: machine, DATVersionID: datID, Closure: closure, Dependencies: dependencies,
		MissingEntries: []string{}, MismatchedEntries: []string{}, Warnings: warnings,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode projected Arcade snapshot: %w", err)
	}
	if err := validateLockedArcadeSnapshot(string(encoded), contentLogicalName, datID); err != nil {
		return "", err
	}
	return string(encoded), nil
}

type arcadeMachineRelation struct {
	cloneOf string
	romOf   string
}

func (service *Service) arcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]legacyArcadeClosureNode, error) {
	if machine == "" {
		return nil, corevalidation.ErrInvalidSnapshot
	}
	traversal := arcadeClosureTraversal{
		ctx: ctx, service: service, datID: datID,
		cache:   make(map[string]arcadeMachineRelation),
		nodes:   []legacyArcadeClosureNode{{Machine: machine, Kind: "CONTENT", Depth: 0}},
		indices: map[string]int{machine: 0}, chain: make(map[string]struct{}),
	}
	if err := traversal.walk(machine); err != nil {
		return nil, err
	}
	sort.Slice(traversal.nodes, func(left, right int) bool {
		if traversal.nodes[left].Depth != traversal.nodes[right].Depth {
			return traversal.nodes[left].Depth < traversal.nodes[right].Depth
		}
		leftKind := legacyArcadeClosureKindOrder(traversal.nodes[left].Kind)
		rightKind := legacyArcadeClosureKindOrder(traversal.nodes[right].Kind)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		return traversal.nodes[left].Machine < traversal.nodes[right].Machine
	})
	return traversal.nodes, nil
}

type arcadeClosureTraversal struct {
	ctx     context.Context
	service *Service
	datID   string
	cache   map[string]arcadeMachineRelation
	nodes   []legacyArcadeClosureNode
	indices map[string]int
	chain   map[string]struct{}
}

func (traversal *arcadeClosureTraversal) resolve(name string) (arcadeMachineRelation, error) {
	return traversal.service.loadLegacyArcadeMachineRelation(
		traversal.ctx, traversal.datID, name, traversal.cache,
	)
}

func (traversal *arcadeClosureTraversal) walk(machine string) error {
	current, depth := machine, 0
	for current != "" {
		if _, duplicate := traversal.chain[current]; duplicate {
			return corevalidation.ErrInvalidSnapshot
		}
		traversal.chain[current] = struct{}{}
		relation, err := traversal.resolve(current)
		if err != nil {
			return err
		}
		if err := traversal.addROMDependency(current, relation, depth); err != nil {
			return err
		}
		if relation.cloneOf == "" {
			break
		}
		if err := traversal.addParent(current, relation.cloneOf, depth); err != nil {
			return err
		}
		current, depth = relation.cloneOf, depth+1
	}
	return nil
}

func (traversal *arcadeClosureTraversal) addROMDependency(
	current string,
	relation arcadeMachineRelation,
	depth int,
) error {
	if relation.romOf == "" || relation.romOf == relation.cloneOf {
		return nil
	}
	if _, err := traversal.resolve(relation.romOf); err != nil {
		return err
	}
	if _, exists := traversal.indices[relation.romOf]; exists {
		return nil
	}
	if len(traversal.nodes) >= 64 {
		return corevalidation.ErrInvalidSnapshot
	}
	requiredBy := current
	traversal.indices[relation.romOf] = len(traversal.nodes)
	traversal.nodes = append(traversal.nodes, legacyArcadeClosureNode{
		Machine: relation.romOf, Kind: "BIOS_OR_BASE", RequiredBy: &requiredBy, Depth: depth + 1,
	})
	return nil
}

func (traversal *arcadeClosureTraversal) addParent(current, parent string, depth int) error {
	if _, duplicate := traversal.chain[parent]; duplicate {
		return corevalidation.ErrInvalidSnapshot
	}
	if _, err := traversal.resolve(parent); err != nil {
		return err
	}
	requiredBy := current
	node := legacyArcadeClosureNode{Machine: parent, Kind: "PARENT", RequiredBy: &requiredBy, Depth: depth + 1}
	if existing, exists := traversal.indices[parent]; exists {
		traversal.nodes[existing] = node
		return nil
	}
	if len(traversal.nodes) >= 64 {
		return corevalidation.ErrInvalidSnapshot
	}
	traversal.indices[parent] = len(traversal.nodes)
	traversal.nodes = append(traversal.nodes, node)
	return nil
}

func (service *Service) loadLegacyArcadeMachineRelation(
	ctx context.Context,
	datID, machine string,
	cache map[string]arcadeMachineRelation,
) (arcadeMachineRelation, error) {
	if relation, exists := cache[machine]; exists {
		return relation, nil
	}
	var cloneOf, romOf sql.NullString
	if err := service.database.QueryRowContext(ctx, `
SELECT cloneof,romof FROM dat_machines WHERE dat_version_id=? AND machine_name=?
`, datID, machine).Scan(&cloneOf, &romOf); err != nil {
		return arcadeMachineRelation{}, fmt.Errorf("load locked DAT machine %q: %w", machine, err)
	}
	relation := arcadeMachineRelation{cloneOf: cloneOf.String, romOf: romOf.String}
	cache[machine] = relation
	return relation, nil
}

func legacyArcadeClosureKindOrder(kind string) int {
	switch kind {
	case "CONTENT":
		return 0
	case "PARENT":
		return 1
	default:
		return 2
	}
}

func (service *Service) legacyArcadeDependencies(
	ctx context.Context,
	revisionID, datID string,
	closure []legacyArcadeClosureNode,
) ([]projectedArcadeDependency, error) {
	byKey, err := service.loadLegacyArcadeDependencyIndex(ctx, revisionID, datID)
	if err != nil {
		return nil, err
	}
	result := make([]projectedArcadeDependency, 0, len(closure)-1)
	for _, node := range closure {
		if node.Kind == "CONTENT" {
			continue
		}
		key := node.Kind + "\x00" + node.Machine
		dependency, exists := byKey[key]
		if !exists {
			return nil, corevalidation.ErrInvalidSnapshot
		}
		delete(byKey, key)
		dependency.RequiredBy, dependency.Depth = node.RequiredBy, node.Depth
		if err := service.validateLegacyArcadeParentFile(ctx, revisionID, dependency); err != nil {
			return nil, err
		}
		result = append(result, dependency)
	}
	if len(byKey) != 0 {
		return nil, corevalidation.ErrInvalidSnapshot
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		if result[left].Depth != result[right].Depth {
			return result[left].Depth < result[right].Depth
		}
		return result[left].Machine < result[right].Machine
	})
	return result, nil
}

func (service *Service) loadLegacyArcadeDependencyIndex(
	ctx context.Context,
	revisionID, datID string,
) (map[string]projectedArcadeDependency, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT kind,logical_archive,source_machine_name,required_entries_json,state
FROM variant_dependencies
WHERE game_variant_revision_id=? AND dat_version_id=?
ORDER BY kind,logical_archive
`, revisionID, datID)
	if err != nil {
		return nil, fmt.Errorf("load legacy Arcade dependencies: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	byKey := make(map[string]projectedArcadeDependency)
	for rows.Next() {
		var kind, logicalArchive, machine, rawEntries, state string
		if err := rows.Scan(&kind, &logicalArchive, &machine, &rawEntries, &state); err != nil {
			return nil, fmt.Errorf("scan legacy Arcade dependency: %w", err)
		}
		if logicalArchive != machine+".zip" ||
			(state != "SATISFIED_BY_CONTENT" && state != "SATISFIED_EXTERNAL" && state != "HASH_WARNING") {
			return nil, corevalidation.ErrInvalidSnapshot
		}
		entries, err := legacyArcadeEntryNames(rawEntries)
		if err != nil {
			return nil, err
		}
		key := kind + "\x00" + machine
		if _, duplicate := byKey[key]; duplicate {
			return nil, corevalidation.ErrInvalidSnapshot
		}
		byKey[key] = projectedArcadeDependency{
			Kind: kind, Machine: machine, State: state,
			ExpectedLogicalName: logicalArchive, RequiredEntryCount: len(entries), RequiredEntries: entries,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy Arcade dependencies: %w", err)
	}
	return byKey, nil
}

func (service *Service) validateLegacyArcadeParentFile(
	ctx context.Context,
	revisionID string,
	dependency projectedArcadeDependency,
) error {
	if dependency.Kind != "PARENT" || dependency.State != "SATISFIED_EXTERNAL" {
		return nil
	}
	var fileCount int
	if err := service.database.QueryRowContext(ctx, `
SELECT count(*) FROM variant_files
WHERE game_variant_revision_id=? AND role='PARENT' AND logical_name=?
`, revisionID, dependency.ExpectedLogicalName).Scan(&fileCount); err != nil {
		return fmt.Errorf("check locked legacy Arcade parent: %w", err)
	}
	if fileCount != 1 {
		return corevalidation.ErrInvalidSnapshot
	}
	return nil
}

func legacyArcadeEntryNames(raw string) ([]string, error) {
	var legacy []string
	if err := json.Unmarshal([]byte(raw), &legacy); err == nil && legacy != nil {
		for _, name := range legacy {
			if name == "" {
				return nil, corevalidation.ErrInvalidSnapshot
			}
		}
		return legacy, nil
	}
	var snapshot struct {
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil || snapshot.Entries == nil {
		return nil, corevalidation.ErrInvalidSnapshot
	}
	names := make([]string, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if entry.Name == "" {
			return nil, corevalidation.ErrInvalidSnapshot
		}
		names = append(names, entry.Name)
	}
	return names, nil
}
