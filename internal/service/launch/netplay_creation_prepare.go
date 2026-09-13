package launch

import (
	"context"
	"crypto/subtle"
	"fmt"

	"github.com/google/uuid"
)

func (service *NetplayCreator) prepare(
	ctx context.Context,
	request NetplayCreateRequest,
) (netplayCreationPrepared, error) {
	snapshot, err := service.repository.Snapshot(ctx, request)
	if err != nil {
		return netplayCreationPrepared{}, fmt.Errorf("read netplay snapshot: %w", err)
	}
	if err := validateNetplayCreation(request, snapshot); err != nil {
		return netplayCreationPrepared{}, err
	}
	if err := service.validateProvider(snapshot.Product.Source, request.ClientCapabilities); err != nil {
		return netplayCreationPrepared{}, err
	}
	content, err := BuildProductContent(snapshot.Product)
	if err != nil {
		return netplayCreationPrepared{}, fmt.Errorf("prepare netplay content: %w", err)
	}
	if len(content.Files) != 1 || len(content.Discs) != 0 {
		return netplayCreationPrepared{}, ErrBlocked
	}
	if service.blobs != nil {
		for _, check := range content.Checks {
			if err := service.blobs.Verify(ctx, check); err != nil {
				return netplayCreationPrepared{}, fmt.Errorf("verify netplay content: %w", err)
			}
		}
	}
	prepared := netplayCreationPrepared{
		snapshot: snapshot,
		plan:     NetplayCreationPlan{Request: request, Source: snapshot.Product.Source, Content: content},
	}
	if snapshot.Existing == nil {
		if err := service.prepareNew(&prepared); err != nil {
			return netplayCreationPrepared{}, err
		}
	}
	// The first clock callback remains outside the writer, allowing reentrant request observers.
	_ = service.environment.Now()
	if err := ctx.Err(); err != nil {
		return netplayCreationPrepared{}, fmt.Errorf("prepare netplay cancelled: %w", err)
	}
	return prepared, nil
}

func (service *NetplayCreator) prepareNew(prepared *netplayCreationPrepared) error {
	content := prepared.plan.Content
	external, err := FreezeProductExternalBIOS(ProductExternalSnapshot{
		DependencySnapshot: prepared.snapshot.Product.Source.DependencySnapshot, ContentName: content.Files[0].LogicalName,
	}, false)
	if err != nil {
		return fmt.Errorf("prepare netplay external BIOS: %w", err)
	}
	external = append(external, ProductBundleFiles(prepared.snapshot.Product.VariantFiles)...)
	prepared.plan.External = external
	id, err := checkedProductID(service.environment.NewID)
	if err != nil {
		return fmt.Errorf("netplay launch identity: %w", err)
	}
	capability, hash, err := service.sign(id)
	if err != nil {
		return err
	}
	prepared.plan.ID, prepared.plan.CredentialHash, prepared.capability = id, hash, capability
	return nil
}

func (service *NetplayCreator) validateProvider(source ProductSource, capabilities Capabilities) error {
	if service.provider == nil {
		return ErrBlocked
	}
	target, found := service.provider.Target(source.ProviderID, source.TargetID)
	bundle, hasBundle := service.provider.BundleSHA256(source.ProviderID, source.TargetID)
	if !found || !hasBundle || bundle != source.BundleSHA256 || !target.Capabilities.NetplayPort {
		return ErrBlocked
	}
	if target.Capabilities.RequiresThreads &&
		(!capabilities.SecureContext || !capabilities.CrossOriginIsolated || !capabilities.SharedArrayBuffer) {
		return ErrBlocked
	}
	return nil
}

func (service *NetplayCreator) sign(id string) (string, []byte, error) {
	if service.environment.SignCapability == nil {
		return "", nil, ErrBlocked
	}
	capability, hash, err := service.environment.SignCapability(id)
	if err != nil {
		return "", nil, fmt.Errorf("sign netplay launch: %w", err)
	}
	if capability == "" || len(hash) != 32 {
		return "", nil, ErrBlocked
	}
	return capability, hash, nil
}

func (service *NetplayCreator) existingResult(existing NetplayExistingLaunch) (Created, error) {
	parsed, err := uuid.Parse(existing.ID)
	if err != nil || parsed.Version() != 7 || parsed.String() != existing.ID {
		return Created{}, ErrBlocked
	}
	capability, hash, err := service.sign(existing.ID)
	if err != nil {
		return Created{}, err
	}
	if subtle.ConstantTimeCompare(hash, existing.CredentialHash) != 1 {
		return Created{}, ErrBlocked
	}
	result := netplayCreated(existing.ID, existing.BootstrapEnd, existing.HardEnd)
	result.Capability, result.Existing = capability, true
	return result, nil
}
