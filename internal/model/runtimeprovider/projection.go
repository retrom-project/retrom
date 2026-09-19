package runtimeprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/capability/runtime/runtimebundle"
	"retrom/internal/capability/runtime/runtimecatalog"
	runtimecontract "retrom/internal/model/runtimecontract"
)

var (
	ErrProjectionInvalid            = errors.New("RUNTIME_PROVIDER_PROJECTION_INVALID")
	ErrProviderDowngrade            = errors.New("RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN")
	ErrProviderVersionRebuilt       = errors.New("RUNTIME_PROVIDER_VERSION_REBUILT")
	ErrProviderTargetReferenced     = errors.New("RUNTIME_PROVIDER_TARGET_REFERENCED")
	ErrProviderCheckpointUnreadable = errors.New("RUNTIME_PROVIDER_CHECKPOINT_FORMAT_UNREADABLE")
)

// Projection contains the validated runtime catalog proposed for activation.
type Projection struct {
	Providers     []ProviderProjection
	Bindings      []runtimecontract.Binding
	Definitions   runtimecontract.Definitions
	CatalogSHA256 string
}

type ProviderProjection struct {
	Active  runtimebundle.ActiveProvider
	Source  string
	Release *runtimebundle.ReleaseIdentity
	Targets []TargetProjection
}

type TargetProjection struct {
	Target            runtimebundle.Target
	CapabilitiesJSON  string
	CheckpointJSON    *string
	TargetOptionsJSON string
	ManifestFragment  string
}

type CurrentProvider struct {
	Version      string
	BundleSHA256 string
}

func NewProjection(
	active runtimebundle.ActiveDescriptor,
	manifests map[string]runtimebundle.Manifest,
	catalog runtimecontract.Catalog,
) (Projection, error) {
	if catalog.SchemaVersion != 1 || len(active.Providers) == 0 ||
		len(active.Providers) != len(manifests) {
		return Projection{}, ErrProjectionInvalid
	}
	catalogContents, err := json.Marshal(catalog)
	if err != nil {
		return Projection{}, projectionInvalid(err)
	}
	catalog, err = runtimecatalog.ParseCatalog(catalogContents)
	if err != nil {
		return Projection{}, projectionInvalid(err)
	}
	targetExists := make(map[string]bool)
	providers := make([]ProviderProjection, 0, len(active.Providers))
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
	if err := validateHostOptionStrategies(catalog, providers); err != nil {
		return Projection{}, err
	}
	return Projection{
		Providers: providers, Bindings: append([]runtimecontract.Binding(nil), catalog.Bindings...),
		Definitions:   catalog.Definitions,
		CatalogSHA256: projectionDigest(catalogContents),
	}, nil
}

func projectProvider(
	active runtimebundle.ActiveDescriptor,
	provider runtimebundle.ActiveProvider,
	manifest runtimebundle.Manifest,
	targetExists map[string]bool,
) (ProviderProjection, error) {
	if manifest.ProviderID != provider.ProviderID || manifest.ProviderVersion != provider.ProviderVersion ||
		manifest.ProviderAPI != provider.ProviderAPI || manifest.ClientModulePath != provider.ClientModulePath ||
		len(manifest.Targets) != len(provider.Targets) {
		return ProviderProjection{}, ErrProjectionInvalid
	}
	byID := make(map[string]runtimebundle.ActiveTarget, len(provider.Targets))
	for _, target := range provider.Targets {
		byID[target.ID] = target
	}
	projected := ProviderProjection{Active: provider, Source: active.Source, Release: active.Release}
	for _, target := range manifest.Targets {
		identity := provider.ProviderID + "\x00" + target.ID
		activeTarget, exists := byID[target.ID]
		if !exists || targetExists[identity] || !targetMatchesActiveProjection(target, activeTarget) {
			return ProviderProjection{}, ErrProjectionInvalid
		}
		projectedTarget, err := projectTarget(target)
		if err != nil {
			return ProviderProjection{}, err
		}
		projected.Targets = append(projected.Targets, projectedTarget)
		targetExists[identity] = true
	}
	return projected, nil
}

func targetMatchesActiveProjection(target runtimebundle.Target, active runtimebundle.ActiveTarget) bool {
	return equalCheckpoint(active.Checkpoint, target.Checkpoint)
}

func projectTarget(target runtimebundle.Target) (TargetProjection, error) {
	capabilities, err := json.Marshal(target.Capabilities)
	if err != nil {
		return TargetProjection{}, projectionInvalid(err)
	}
	var checkpointJSON *string
	if target.Checkpoint != nil {
		contents, marshalErr := json.Marshal(target.Checkpoint)
		if marshalErr != nil {
			return TargetProjection{}, projectionInvalid(marshalErr)
		}
		value := string(contents)
		checkpointJSON = &value
	}
	fragment, err := json.Marshal(target)
	if err != nil {
		return TargetProjection{}, projectionInvalid(err)
	}
	var frozen runtimebundle.Target
	if err := json.Unmarshal(fragment, &frozen); err != nil {
		return TargetProjection{}, projectionInvalid(err)
	}
	optionsSchema, err := json.Marshal(target.TargetOptionsSchema)
	if err != nil {
		return TargetProjection{}, projectionInvalid(err)
	}
	return TargetProjection{
		Target: frozen, CapabilitiesJSON: string(capabilities), CheckpointJSON: checkpointJSON,
		TargetOptionsJSON: string(optionsSchema), ManifestFragment: string(fragment),
	}, nil
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
