package launch

import (
	"context"
	"fmt"
)

func (service *ProductCreator) preparePlan(
	ctx context.Context,
	command ProductCreateCommand,
	snapshot ProductSnapshot,
) (ProductCreatePlan, string, error) {
	selectedDOS, err := selectedProductDOS(command, snapshot)
	if err != nil {
		return ProductCreatePlan{}, "", err
	}
	approved, override, err := approvedProductBIOS(snapshot)
	if err != nil {
		return ProductCreatePlan{}, "", err
	}
	content, err := BuildProductContent(approved)
	if err != nil {
		return ProductCreatePlan{}, "", err
	}
	if service.blobs != nil {
		for _, check := range content.Checks {
			if err := service.blobs.Verify(ctx, check); err != nil {
				return ProductCreatePlan{}, "", fmt.Errorf("verify product content: %w", err)
			}
		}
	}
	initialDisc, err := productInitialDisc(snapshot, len(content.Discs))
	if err != nil {
		return ProductCreatePlan{}, "", err
	}
	external, err := productExternalFiles(approved, content)
	if err != nil {
		return ProductCreatePlan{}, "", err
	}
	id, err := checkedProductID(service.environment.NewID)
	if err != nil {
		return ProductCreatePlan{}, "", err
	}
	capability, hash, err := service.environment.SignCapability(id)
	if err != nil {
		return ProductCreatePlan{}, "", fmt.Errorf("sign product capability: %w", err)
	}
	plan := ProductCreatePlan{
		Command: command, Source: approved.Source, Content: content, External: external, ID: id,
		CredentialHash: hash, SelectedDOSEntry: selectedDOS.Path, InitialDiscIndex: initialDisc, OverrideBIOS: override,
	}
	if snapshot.Source.DeliveryProfile == "ISOLATED_WEB_PROJECT" {
		if service.environment.SignIsolation == nil {
			return ProductCreatePlan{}, "", ErrBlocked
		}
		ticket, err := service.environment.SignIsolation(id)
		if err != nil {
			return ProductCreatePlan{}, "", fmt.Errorf("sign product isolation: %w", err)
		}
		plan.Isolation = &ticket
	}
	return plan, capability, nil
}
