package launch

import (
	"context"
	"fmt"
	"time"
)

type ProductCreator struct {
	repository  ProductCreationRepository
	provider    PreviewProvider
	blobs       ProductBlobVerifier
	environment ProductEnvironment
}

func NewProductCreator(
	repository ProductCreationRepository,
	provider PreviewProvider,
	blobs ProductBlobVerifier,
	environment ProductEnvironment,
) *ProductCreator {
	if environment.NewID == nil {
		environment.NewID = newProductID
	}
	if environment.Now == nil {
		environment.Now = time.Now
	}
	return &ProductCreator{repository: repository, provider: provider, blobs: blobs, environment: environment}
}

type productPreparation struct {
	snapshot   ProductSnapshot
	plan       ProductCreatePlan
	capability string
	validation bool
}
type productAttempt struct {
	receipt ProductReceipt
	ready   bool
	resume  string
}

func (service *ProductCreator) Create(ctx context.Context, command ProductCreateCommand) (ProductReceipt, error) {
	if !validProductRequest(command) {
		return ProductReceipt{}, ErrBlocked
	}
	stored, found, err := service.repository.Replay(ctx, command)
	if err != nil {
		return ProductReceipt{}, fmt.Errorf("read product replay: %w", err)
	}
	if found {
		return service.replay(command, stored)
	}
	for range 2 {
		preparation, err := service.prepare(ctx, command)
		if err != nil {
			return ProductReceipt{}, err
		}
		var outcome productAttempt
		err = service.repository.WithCreation(ctx, func(scope ProductCreationScope) error {
			var commitErr error
			outcome, commitErr = service.commit(ctx, scope, command, preparation)
			return commitErr
		})
		if err != nil {
			return ProductReceipt{}, fmt.Errorf("create product: %w", err)
		}
		if outcome.ready {
			continue
		}
		if outcome.resume != "" && service.environment.ResumeValidation != nil {
			service.environment.ResumeValidation(context.WithoutCancel(ctx), outcome.resume)
		}
		return outcome.receipt, nil
	}
	return ProductReceipt{}, ErrBlocked
}

func (service *ProductCreator) prepare(ctx context.Context, command ProductCreateCommand) (productPreparation, error) {
	snapshot, err := service.repository.Snapshot(ctx, command)
	if err != nil {
		return productPreparation{}, fmt.Errorf("read product snapshot: %w", err)
	}
	if err := validateProductSelection(command, snapshot); err != nil {
		return productPreparation{}, err
	}
	if err := service.validateProvider(snapshot.Source, command.Request.ClientCapabilities); err != nil {
		return productPreparation{}, err
	}
	fresh := false
	if snapshot.Source.VariantStatus == "READY" {
		fresh, err = productBIOSFresh(snapshot)
		if err != nil {
			return productPreparation{}, err
		}
	}
	preparation := productPreparation{snapshot: snapshot, validation: !fresh}
	if fresh {
		preparation.plan, preparation.capability, err = service.preparePlan(ctx, command, snapshot)
		if err != nil {
			return productPreparation{}, err
		}
	}
	// Provider, CAS, signing and the first clock invocation happen before a writer opens.
	preparation.plan.NowMS = service.environment.Now().UnixMilli()
	return preparation, nil
}

func (service *ProductCreator) validateProvider(source ProductSource, capabilities Capabilities) error {
	if service.provider == nil {
		return ErrBlocked
	}
	target, found := service.provider.Target(source.ProviderID, source.TargetID)
	bundle, hasBundle := service.provider.BundleSHA256(source.ProviderID, source.TargetID)
	if !found || !hasBundle || bundle != source.BundleSHA256 {
		return ErrBlocked
	}
	if target.Capabilities.RequiresThreads && (!capabilities.SecureContext ||
		!capabilities.CrossOriginIsolated || !capabilities.SharedArrayBuffer) {
		return ErrBlocked
	}
	return nil
}
