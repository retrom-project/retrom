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
	if err := service.describeResourceBundles(ctx, snapshot.Files, resources); err != nil {
		return Config{}, err
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

func (service *ConfigIssuer) describeResourceBundles(
	ctx context.Context, files []ConfigFile, resources []map[string]any,
) error {
	for _, resource := range resources {
		if resource["kind"] != "PARENT_ARCHIVE" && resource["kind"] != "BIOS_BUNDLE" {
			continue
		}
		if service.environment.DescribeBundle == nil {
			return ErrBlocked
		}
		role := "PARENT"
		if resource["kind"] == "BIOS_BUNDLE" {
			role = "BIOS_BUNDLE"
		}
		archive, err := service.environment.DescribeBundle(ctx, configFilesWithRole(files, role))
		if err != nil {
			return fmt.Errorf("describe content bundle: %w", err)
		}
		if !validContentDigest(archive.SHA256) || archive.SizeBytes < 1 {
			return ErrBlocked
		}
		if role == "BIOS_BUNDLE" {
			files, ok := resource["files"].([]map[string]any)
			if !ok || len(files) != 1 {
				return ErrBlocked
			}
			files[0]["sha256"], files[0]["sizeBytes"] = archive.SHA256, archive.SizeBytes
		} else {
			resource["sha256"], resource["sizeBytes"] = archive.SHA256, archive.SizeBytes
		}
	}
	return nil
}
