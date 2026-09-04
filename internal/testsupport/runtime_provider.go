package testsupport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
)

// RuntimeTargetIdentity is the immutable runtime identity persisted by domain
// fixtures. Tests resolve it through the same Host binding projection as
// production code instead of recreating Provider-owned Target facts.
type RuntimeTargetIdentity struct {
	ProviderID      string
	ProviderVersion string
	TargetID        string
	BundleSHA256    string
}

// NewRuntimeBuilder reconstructs the Provider-neutral launch builder from the
// deterministic database projection. Integration tests use it to exercise the
// same Envelope boundary as production without installing filesystem bundles.
func NewRuntimeBuilder(ctx context.Context, database *sql.DB) (*runtimelaunch.Builder, error) {
	rows, err := database.QueryContext(ctx, `
SELECT provider.provider_id,provider.provider_version,provider.provider_api_version,
       provider.bundle_sha256,provider.manifest_sha256,provider.module_sha256,
       target.manifest_fragment_json
FROM runtime_providers provider
LEFT JOIN runtime_targets target ON target.provider_id=provider.provider_id
ORDER BY provider.provider_id,target.target_id
`)
	if err != nil {
		return nil, fmt.Errorf("testsupport: read runtime providers: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	active := runtimebundle.ActiveDescriptor{SchemaVersion: 1, Source: "candidate"}
	manifests := make(map[string]runtimebundle.Manifest)
	providerIndexes := make(map[string]int)
	for rows.Next() {
		var provider runtimebundle.ActiveProvider
		var fragment sql.NullString
		if err := rows.Scan(&provider.ProviderID, &provider.ProviderVersion, &provider.ProviderAPI,
			&provider.BundleSHA256, &provider.ManifestSHA256, &provider.ModuleSHA256,
			&fragment); err != nil {
			return nil, fmt.Errorf("testsupport: scan runtime provider: %w", err)
		}
		providerIndex, exists := providerIndexes[provider.ProviderID]
		if !exists {
			provider.ClientModulePath = "client.mjs"
			active.Providers = append(active.Providers, provider)
			providerIndex = len(active.Providers) - 1
			providerIndexes[provider.ProviderID] = providerIndex
			manifests[provider.ProviderID] = runtimebundle.Manifest{
				SchemaVersion: 1, ProviderID: provider.ProviderID, ProviderVersion: provider.ProviderVersion,
				ProviderAPI: provider.ProviderAPI, ClientModulePath: provider.ClientModulePath,
			}
		}
		if !fragment.Valid {
			continue
		}
		var target runtimebundle.Target
		if err := json.Unmarshal([]byte(fragment.String), &target); err != nil {
			return nil, fmt.Errorf("testsupport: decode runtime target: %w", err)
		}
		manifest := manifests[provider.ProviderID]
		manifest.Targets = append(manifest.Targets, target)
		manifests[provider.ProviderID] = manifest
		active.Providers[providerIndex].Targets = append(active.Providers[providerIndex].Targets,
			runtimebundle.ActiveTarget{ID: target.ID, Checkpoint: target.Checkpoint})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("testsupport: runtime providers: %w", err)
	}
	builder, err := runtimelaunch.NewBuilder(active, manifests)
	if err != nil {
		return nil, fmt.Errorf("testsupport: build runtime envelope builder: %w", err)
	}
	return builder, nil
}

// LookupRuntimeTarget resolves the active Provider Target bound to a Product
// Core in the deterministic test projection.
func LookupRuntimeTarget(
	ctx context.Context,
	database interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	coreID string,
) (RuntimeTargetIdentity, error) {
	var identity RuntimeTargetIdentity
	err := database.QueryRowContext(ctx, `
SELECT binding.provider_id,provider.provider_version,binding.target_id,provider.bundle_sha256
FROM runtime_target_bindings binding
JOIN runtime_targets target
  ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
JOIN runtime_providers provider ON provider.provider_id=binding.provider_id
WHERE binding.core_id=?
ORDER BY binding.binding_id
LIMIT 1
`, coreID).Scan(
		&identity.ProviderID,
		&identity.ProviderVersion,
		&identity.TargetID,
		&identity.BundleSHA256,
	)
	if err != nil {
		return RuntimeTargetIdentity{}, fmt.Errorf("testsupport: resolve runtime target for core %s: %w", coreID, err)
	}
	return identity, nil
}

// SeedRuntimeProviders installs a deterministic Provider projection for domain
// tests. It intentionally contains no implementation or asset mapping; tests
// that exercise the installation boundary use runtimeprovider fixtures instead.
func SeedRuntimeProviders(ctx context.Context, database *sql.DB, catalog runtimecatalog.Catalog) error {
	var existing int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM runtime_providers`).Scan(&existing); err != nil {
		return fmt.Errorf("testsupport: inspect provider projection: %w", err)
	}
	if existing != 0 {
		return nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("testsupport: begin provider projection: %w", err)
	}
	defer func() { cleanup.Error("rollback", transaction.Rollback()) }()
	targets, providerIDs := runtimeProjectionFixtures(catalog)
	if err := insertFixtureProviders(ctx, transaction, providerIDs); err != nil {
		return err
	}
	if err := insertFixtureTargets(ctx, transaction, targets); err != nil {
		return err
	}
	if err := insertFixtureBindings(ctx, transaction, catalog.Bindings); err != nil {
		return err
	}
	if err := insertFixtureCatalogState(ctx, transaction, catalog.CatalogVersion); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("testsupport: commit provider projection: %w", err)
	}
	return nil
}

func runtimeProjectionFixtures(
	catalog runtimecatalog.Catalog,
) (map[string]runtimecatalog.Binding, []string) {
	targets := make(map[string]runtimecatalog.Binding)
	providers := make(map[string]bool)
	for _, binding := range catalog.Bindings {
		key := binding.ProviderID + "\x00" + binding.TargetID
		targets[key] = binding
		providers[binding.ProviderID] = true
	}
	providerIDs := make([]string, 0, len(providers))
	for providerID := range providers {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	return targets, providerIDs
}

func insertFixtureProviders(ctx context.Context, transaction *sql.Tx, providerIDs []string) error {
	for _, providerID := range providerIDs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_providers(
 provider_id,provider_version,provider_api_version,bundle_sha256,manifest_sha256,module_sha256,
 source,release_repository,release_tag,release_commit,activated_at_ms
) VALUES(?,'1.0.0',1,?,?,?,'candidate',NULL,NULL,NULL,0)
`, providerID, fixtureDigest("bundle:"+providerID), fixtureDigest("manifest:"+providerID),
			fixtureDigest("module:"+providerID)); err != nil {
			return fmt.Errorf("testsupport: insert provider %s: %w", providerID, err)
		}
	}
	return nil
}

func insertFixtureTargets(
	ctx context.Context,
	transaction *sql.Tx,
	targets map[string]runtimecatalog.Binding,
) error {
	keys := make([]string, 0, len(targets))
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		binding := targets[key]
		optionsSchema := fixtureTargetOptionsSchema(binding.TargetID)
		validationProbes := []string{}
		if strings.HasPrefix(binding.TargetID, "rpgmaker-") {
			validationProbes = []string{"rpgmaker.position.v1"}
		}
		capabilities := map[string]any{
			"pause": true, "screenshot": true, "checkpoint": true, "standardGamepad": true,
			"frameCounter": true, "volume": true, "discSwitch": binding.TargetID == "yabause",
			"nativeSettings": true, "inputFilter": true, "netplayPort": binding.ProviderID == "emulatorjs",
			"videoModes": []string{"original"}, "requiresThreads": false, "frameMode": "NONE",
			"validationProbes": validationProbes,
		}
		checkpoint := map[string]any{
			"writeFormat": "test-checkpoint-v1", "readFormats": []string{"test-checkpoint-v1"},
			"maxBytes": 268435456,
		}
		capabilitiesJSON, _ := json.Marshal(capabilities)
		checkpointJSON, _ := json.Marshal(checkpoint)
		fragmentJSON, _ := json.Marshal(map[string]any{
			"id": binding.TargetID, "displayName": binding.TargetID,
			"targetOptionsSchema": optionsSchema, "inputs": fixtureInputs(binding), "capabilities": capabilities,
			"checkpoint": checkpoint, "assetPaths": []string{"client.mjs"},
		})
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_targets(
 provider_id,target_id,display_name,target_options_schema_json,capabilities_json,
 checkpoint_json,manifest_fragment_json
) VALUES(?,?,?,?,?,?,?)
`, binding.ProviderID, binding.TargetID, binding.TargetID, string(mustJSON(optionsSchema)),
			string(capabilitiesJSON), string(checkpointJSON), string(fragmentJSON)); err != nil {
			return fmt.Errorf("testsupport: insert target %s/%s: %w", binding.ProviderID, binding.TargetID, err)
		}
	}
	return nil
}

func insertFixtureBindings(
	ctx context.Context,
	transaction *sql.Tx,
	bindings []runtimecatalog.Binding,
) error {
	for _, binding := range bindings {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy,review_policy
) VALUES(?,?,?,?,?,?,?,?)
`, binding.ID, binding.CoreID, binding.ProviderID, binding.TargetID, binding.DetectorProfile,
			binding.DeliveryProfile, binding.LaunchPolicy, binding.ReviewPolicy); err != nil {
			return fmt.Errorf("testsupport: insert runtime binding %s: %w", binding.ID, err)
		}
		for _, platformID := range binding.PlatformIDs {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_binding_platforms(binding_id,platform_id,core_id) VALUES(?,?,?)
`, binding.ID, platformID, binding.CoreID); err != nil {
				return fmt.Errorf("testsupport: insert binding platform: %w", err)
			}
		}
		for _, contentKind := range binding.AcceptedContentKinds {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_binding_content_kinds(binding_id,content_kind) VALUES(?,?)
`, binding.ID, contentKind); err != nil {
				return fmt.Errorf("testsupport: insert binding content kind: %w", err)
			}
		}
	}
	return nil
}

func insertFixtureCatalogState(ctx context.Context, transaction *sql.Tx, catalogVersion int) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_catalog_state(singleton,catalog_version,catalog_sha256,activated_at_ms)
VALUES(1,?, ?,0)
`, catalogVersion, fixtureDigest("catalog")); err != nil {
		return fmt.Errorf("testsupport: insert runtime catalog: %w", err)
	}
	return nil
}

func fixtureInputs(binding runtimecatalog.Binding) []map[string]any {
	if binding.ProviderID == "emulatorjs" {
		return []map[string]any{
			{"role": "game", "kind": "ROM_BLOB", "cardinality": "ONE", "optional": false},
			{"role": "bios", "kind": "BIOS_BUNDLE", "cardinality": "ONE", "optional": true},
			{"role": "parent", "kind": "PARENT_ARCHIVE", "cardinality": "ONE", "optional": true},
			{"role": "discs", "kind": "MULTI_DISC", "cardinality": "ONE", "optional": true},
			{"role": "external", "kind": "EXTERNAL_FILE_SET", "cardinality": "ONE", "optional": true},
		}
	}
	gameKind := "ROM_BLOB"
	switch binding.TargetID {
	case "wasm4":
		gameKind = "WASM4_CART"
	case "rpgmaker-xp", "rpgmaker-vx", "rpgmaker-vx-ace":
		gameKind = "SEEKABLE_BLOB"
	case "butterscotch-gamemaker":
		gameKind = "NATIVE_WEB"
	case "tyranoscript":
		gameKind = "ISOLATED_WEB"
	case "onscripter-yuri", "kirikiri2-kag", "rpgmaker-2000", "rpgmaker-2003":
		gameKind = "FILE_TREE"
	case "rpgmaker-mv", "rpgmaker-mz":
		gameKind = "NATIVE_WEB"
	}
	result := []map[string]any{{"role": "game", "kind": gameKind, "cardinality": "ONE", "optional": false}}
	if strings.HasPrefix(binding.TargetID, "rpgmaker-") {
		result = append(result, map[string]any{
			"role": "rtp", "kind": gameKind, "cardinality": "ONE", "optional": true,
		})
	}
	return result
}

func fixtureTargetOptionsSchema(targetID string) map[string]any {
	property := func(properties map[string]any, required ...string) map[string]any {
		required = append([]string{}, required...)
		return map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": properties, "required": required,
		}
	}
	switch {
	case strings.HasPrefix(targetID, "rpgmaker-"):
		return property(map[string]any{"expectedRestorePosition": map[string]any{
			"type": []any{"object", "null"}, "additionalProperties": false,
			"properties": map[string]any{
				"fixtureState": map[string]any{"type": "integer", "minimum": int64(0)},
				"mapId":        map[string]any{"type": "integer", "minimum": int64(0)},
				"playerX":      map[string]any{"type": "integer", "minimum": int64(0)},
				"playerY":      map[string]any{"type": "integer", "minimum": int64(0)},
			}, "required": []any{"fixtureState", "mapId", "playerX", "playerY"},
		}}, "expectedRestorePosition")
	case targetID == "onscripter-yuri":
		return property(map[string]any{"scriptEncoding": map[string]any{
			"type": "string", "enum": []any{"gbk", "sjis", "utf8"},
		}}, "scriptEncoding")
	case targetID == "kirikiri2-kag":
		return property(map[string]any{"startupXp3Path": map[string]any{
			"type": []any{"string", "null"}, "format": "safe-path", "maxLength": int64(240),
		}}, "startupXp3Path")
	case targetID == "butterscotch-gamemaker" || targetID == "tyranoscript" || targetID == "wasm4":
		return property(map[string]any{})
	default:
		return property(map[string]any{
			"dosEntryPath": map[string]any{
				"type": []any{"string", "null"}, "format": "safe-path", "maxLength": int64(240),
			},
			"initialDiscIndex": map[string]any{"type": []any{"integer", "null"}, "minimum": int64(0)},
		}, "dosEntryPath", "initialDiscIndex")
	}
}

func mustJSON(value any) []byte {
	contents, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return contents
}

func fixtureDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
