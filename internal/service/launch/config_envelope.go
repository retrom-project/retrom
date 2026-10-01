package launch

import (
	"context"
	"fmt"

	runtimecatalog "retrom/internal/runtime/catalog"
	runtimelaunch "retrom/internal/runtime/launch"
)

func (service *ConfigIssuer) envelope(
	ctx context.Context, id string, snapshot ConfigSnapshot, ticket IsolationTicket,
) (Config, error) {
	source := snapshot.Authority.Source
	if service.runtimeBuilder == nil {
		return Config{}, ErrCredential
	}
	target, exists := service.runtimeBuilder.Target(source.ProviderID, source.TargetID)
	bundle, bundleExists := service.runtimeBuilder.BundleSHA256(source.ProviderID, source.TargetID)
	if !exists || !bundleExists || bundle != source.BundleDigest {
		return Config{}, ErrCredential
	}
	resources, err := providerResources(snapshot, target, ticket)
	if err != nil {
		return Config{}, err
	}
	for _, resource := range resources {
		if resource["kind"] != "PARENT_ARCHIVE" {
			continue
		}
		if service.environment.DescribeBundle == nil {
			return Config{}, ErrBlocked
		}
		archive, err := service.environment.DescribeBundle(ctx, configFilesWithRole(snapshot.Files, "PARENT"))
		if err != nil {
			return Config{}, fmt.Errorf("describe parent archive: %w", err)
		}
		if !validContentDigest(archive.SHA256) || archive.SizeBytes < 1 {
			return Config{}, ErrBlocked
		}
		resource["sha256"], resource["sizeBytes"] = archive.SHA256, archive.SizeBytes
	}
	restore, _, err := providerRestore(id, snapshot.Authority.Restore, target)
	if err != nil {
		return Config{}, err
	}
	options, err := providerTargetOptions(target.TargetOptionsSchema, source)
	if err != nil {
		return Config{}, err
	}
	contents, err := service.runtimeBuilder.Build(runtimelaunch.Input{
		Binding: runtimecatalog.Binding{
			ProviderID: source.ProviderID, TargetID: source.TargetID, CoreID: source.CoreID, LaunchPolicy: "SUPPORTED",
		},
		Session: runtimelaunch.Session{
			ID: id, Purpose: source.Purpose, Mode: "SINGLE", Title: source.Title, PlatformName: source.PlatformName,
			CoreName: source.CoreName, ReturnTo: source.ReturnTo, Warnings: providerWarnings(source),
		},
		Resources: resources, TargetOptions: options, Restore: restore,
	})
	if err != nil {
		return Config{}, fmt.Errorf("build Provider launch envelope: %w", err)
	}
	return Config{contents: contents}, nil
}
