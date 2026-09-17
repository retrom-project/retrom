package launch

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/launch"
)

type ProductCreator struct {
	repository  model.ProductCreationRepository
	provider    model.PreviewProvider
	blobs       model.ProductBlobVerifier
	environment model.ProductEnvironment
}

func NewProductCreator(
	repository model.ProductCreationRepository,
	provider model.PreviewProvider,
	blobs model.ProductBlobVerifier,
	environment model.ProductEnvironment,
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
	snapshot   model.ProductSnapshot
	plan       model.ProductCreatePlan
	capability string
	validation bool
}
type productAttempt struct {
	receipt model.ProductReceipt
	ready   bool
	resume  string
}

func (service *ProductCreator) Create(ctx context.Context, command model.ProductCreateCommand) (model.ProductReceipt, error) {
	if !validProductRequest(command) {
		return model.ProductReceipt{}, model.ErrBlocked
	}
	stored, found, err := service.repository.Replay(ctx, command)
	if err != nil {
		return model.ProductReceipt{}, fmt.Errorf("read product replay: %w", err)
	}
	if found {
		return service.replay(command, stored)
	}
	for range 2 {
		preparation, err := service.prepare(ctx, command)
		if err != nil {
			return model.ProductReceipt{}, err
		}
		var outcome productAttempt
		err = service.repository.WithCreation(ctx, func(scope model.ProductCreationScope) error {
			var commitErr error
			outcome, commitErr = service.commit(ctx, scope, command, preparation)
			return commitErr
		})
		if err != nil {
			return model.ProductReceipt{}, fmt.Errorf("create product: %w", err)
		}
		if outcome.ready {
			continue
		}
		if outcome.resume != "" && service.environment.ResumeValidation != nil {
			service.environment.ResumeValidation(context.WithoutCancel(ctx), outcome.resume)
		}
		return outcome.receipt, nil
	}
	return model.ProductReceipt{}, model.ErrBlocked
}

func (service *ProductCreator) prepare(ctx context.Context, command model.ProductCreateCommand) (productPreparation, error) {
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

func (service *ProductCreator) validateProvider(source model.ProductSource, capabilities model.Capabilities) error {
	if service.provider == nil {
		return model.ErrBlocked
	}
	target, found := service.provider.Target(source.ProviderID, source.TargetID)
	bundle, hasBundle := service.provider.BundleSHA256(source.ProviderID, source.TargetID)
	if !found || !hasBundle || bundle != source.BundleSHA256 {
		return model.ErrBlocked
	}
	if target.Capabilities.RequiresThreads && (!capabilities.SecureContext ||
		!capabilities.CrossOriginIsolated || !capabilities.SharedArrayBuffer) {
		return model.ErrBlocked
	}
	return nil
}
