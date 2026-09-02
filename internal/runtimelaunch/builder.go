// Package runtimelaunch builds the single Provider-neutral launch boundary.
package runtimelaunch

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
)

var ErrEnvelopeInvalid = errors.New("RUNTIME_LAUNCH_ENVELOPE_INVALID")

type Session struct {
	ID           string
	Purpose      string
	Mode         string
	Title        string
	PlatformName string
	ReturnTo     string
	Warnings     []string
}

type Input struct {
	Binding       runtimecatalog.Binding
	Session       Session
	Resources     []map[string]any
	TargetOptions map[string]any
	Restore       any
	Validation    any
	Netplay       any
}

type Builder struct {
	targets map[string]resolvedTarget
}

type resolvedTarget struct {
	provider runtimebundle.ActiveProvider
	active   runtimebundle.ActiveTarget
	target   runtimebundle.Target
}

func NewBuilder(active runtimebundle.ActiveDescriptor, manifests map[string]runtimebundle.Manifest) (*Builder, error) {
	result := &Builder{targets: make(map[string]resolvedTarget)}
	for _, provider := range active.Providers {
		manifest, exists := manifests[provider.ProviderID]
		if !exists || manifest.ProviderID != provider.ProviderID || manifest.ProviderVersion != provider.ProviderVersion ||
			manifest.ProviderAPI != provider.ProviderAPI || manifest.ClientModulePath != provider.ClientModulePath {
			return nil, ErrEnvelopeInvalid
		}
		manifestTargets := make(map[string]runtimebundle.Target, len(manifest.Targets))
		for _, target := range manifest.Targets {
			manifestTargets[target.ID] = target
		}
		if len(manifestTargets) != len(provider.Targets) {
			return nil, ErrEnvelopeInvalid
		}
		for _, activeTarget := range provider.Targets {
			target, exists := manifestTargets[activeTarget.ID]
			if !exists || target.GameCompatibilityLine != activeTarget.GameCompatibilityLine ||
				target.NetplayCompatibilityLine == nil != (activeTarget.NetplayCompatibilityLine == nil) ||
				target.NetplayCompatibilityLine != nil && *target.NetplayCompatibilityLine != *activeTarget.NetplayCompatibilityLine ||
				target.ContractSHA256 != activeTarget.ContractSHA256 || !reflect.DeepEqual(target.Checkpoint, activeTarget.Checkpoint) {
				return nil, ErrEnvelopeInvalid
			}
			key := provider.ProviderID + "\x00" + target.ID
			if _, duplicate := result.targets[key]; duplicate {
				return nil, ErrEnvelopeInvalid
			}
			result.targets[key] = resolvedTarget{provider: provider, active: activeTarget, target: target}
		}
	}
	if len(result.targets) == 0 {
		return nil, ErrEnvelopeInvalid
	}
	return result, nil
}

func (builder *Builder) Build(input Input) ([]byte, error) {
	if builder == nil || input.Binding.LaunchPolicy == "DISABLED" {
		return nil, ErrEnvelopeInvalid
	}
	resolved, exists := builder.targets[input.Binding.ProviderID+"\x00"+input.Binding.TargetID]
	if !exists || !validResources(resolved.target.Inputs, input.Resources) ||
		input.TargetOptions == nil || input.TargetOptions["kind"] != resolved.target.OptionsKind {
		return nil, ErrEnvelopeInvalid
	}
	warnings := input.Session.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	base := fmt.Sprintf("/runtime/providers/%s/%s/", resolved.provider.ProviderID, resolved.provider.BundleSHA256)
	envelope := map[string]any{
		"schemaVersion": 1,
		"session": map[string]any{
			"id": input.Session.ID, "purpose": input.Session.Purpose, "mode": input.Session.Mode,
			"title": input.Session.Title, "platformName": input.Session.PlatformName,
			"returnTo": input.Session.ReturnTo, "warnings": warnings,
		},
		"runtime": map[string]any{
			"providerId": resolved.provider.ProviderID, "providerVersion": resolved.provider.ProviderVersion,
			"providerApiVersion": resolved.provider.ProviderAPI, "targetId": resolved.target.ID,
			"bundleSha256": resolved.provider.BundleSHA256, "moduleSha256": resolved.provider.ModuleSHA256,
			"targetContractSha256":  resolved.active.ContractSHA256,
			"gameCompatibilityLine": resolved.active.GameCompatibilityLine,
			"runtimeBaseUrl":        base, "moduleUrl": base + resolved.provider.ClientModulePath,
			"capabilities": resolved.target.Capabilities, "checkpoint": resolved.target.Checkpoint,
		},
		"resources": input.Resources, "targetOptions": input.TargetOptions,
		"restore": input.Restore, "validation": input.Validation, "netplay": input.Netplay,
	}
	contents, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvelopeInvalid, err)
	}
	if _, err := runtimebundle.ParseLaunchEnvelope(contents); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvelopeInvalid, err)
	}
	return contents, nil
}

func validResources(inputs []runtimebundle.Input, resources []map[string]any) bool {
	byRole := make(map[string]runtimebundle.Input, len(inputs))
	counts := make(map[string]int, len(inputs))
	for _, input := range inputs {
		byRole[input.Role] = input
	}
	for _, resource := range resources {
		role, roleOK := resource["role"].(string)
		kind, kindOK := resource["kind"].(string)
		declaration, declared := byRole[role]
		if !roleOK || !kindOK || !declared || declaration.Kind != kind {
			return false
		}
		counts[role]++
	}
	for _, input := range inputs {
		count := counts[input.Role]
		if !input.Optional && count == 0 || input.Cardinality == "ONE" && count > 1 {
			return false
		}
	}
	return true
}
