package launch

import (
	"fmt"

	"retrom/internal/capability/runtime/runtimelaunch"
	model "retrom/internal/model/launch"
	runtimecontract "retrom/internal/model/runtimecontract"
)

func (service *ConfigIssuer) envelope(
	id string,
	snapshot model.ConfigSnapshot,
	ticket model.IsolationTicket,
) (Config, error) {
	source := snapshot.Authority.Source
	if service.runtimeBuilder == nil {
		return Config{}, model.ErrCredential
	}
	target, exists := service.runtimeBuilder.Target(source.ProviderID, source.TargetID)
	bundle, bundleExists := service.runtimeBuilder.BundleSHA256(source.ProviderID, source.TargetID)
	if !exists || !bundleExists || bundle != source.BundleDigest {
		return Config{}, model.ErrCredential
	}
	resources, err := providerResources(snapshot, target, ticket)
	if err != nil {
		return Config{}, err
	}
	restore, _, err := providerRestore(id, snapshot.Authority.Restore, target)
	if err != nil {
		return Config{}, err
	}
	netplay, mode, err := providerNetplay(service.environment.PublicOrigin, source)
	if err != nil {
		return Config{}, err
	}
	options, err := providerTargetOptions(target.TargetOptionsSchema, source)
	if err != nil {
		return Config{}, err
	}
	contents, err := service.runtimeBuilder.Build(runtimelaunch.Input{
		Binding: runtimecontract.Binding{
			ProviderID: source.ProviderID, TargetID: source.TargetID, CoreID: source.CoreID, LaunchPolicy: "SUPPORTED",
		},
		Session: runtimelaunch.Session{
			ID: id, Purpose: source.Purpose, Mode: mode, Title: source.Title, PlatformName: source.PlatformName,
			CoreName: source.CoreName, ReturnTo: source.ReturnTo, Warnings: providerWarnings(source),
		},
		Resources: resources, TargetOptions: options, Restore: restore, Netplay: netplay,
	})
	if err != nil {
		return Config{}, fmt.Errorf("build Provider launch envelope: %w", err)
	}
	return Config{contents: contents}, nil
}
