package launch

import (
	"context"

	"retrom/internal/runtimebundle"
)

func (service *Service) providerInputResources(
	ctx context.Context,
	sessionID, capability string,
	source providerConfigSource,
	input runtimebundle.Input,
	files []lockedProviderFile,
) ([]map[string]any, error) {
	if input.Role == "rtp" {
		values, err := service.providerRuntimePackResources(ctx, sessionID, capability, input.Kind)
		if err != nil || !input.Optional && len(values) == 0 {
			return nil, ErrCredential
		}
		return values, nil
	}
	value, err := service.providerSingleInputResource(ctx, sessionID, capability, source, input, files)
	if err != nil {
		if input.Optional {
			return nil, nil
		}
		return nil, err
	}
	return []map[string]any{value}, nil
}

func (service *Service) providerSingleInputResource(
	ctx context.Context,
	sessionID, capability string,
	source providerConfigSource,
	input runtimebundle.Input,
	files []lockedProviderFile,
) (map[string]any, error) {
	switch input.Role {
	case "game":
		return service.providerGameResource(ctx, sessionID, capability, source, input.Kind, files)
	case "bios":
		return service.providerBundleResource(
			ctx, sessionID, capability, "BIOS_BUNDLE", "bios", input.Kind,
		)
	case "parent":
		return service.providerParentResource(ctx, sessionID, capability, input.Kind)
	case "external":
		return service.providerExternalResource(ctx, sessionID, input.Kind)
	case "discs":
		return service.providerDiscResource(ctx, sessionID, source.initialDisc, input.Kind)
	default:
		return nil, ErrCredential
	}
}
