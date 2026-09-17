package launch

import (
	"context"
	"fmt"
	model "retrom/internal/model/launch"
)

func (service *ProductCreator) preparePlan(
	ctx context.Context,
	command model.ProductCreateCommand,
	snapshot model.ProductSnapshot,
) (model.ProductCreatePlan, string, error) {
	selectedDOS, err := selectedProductDOS(command, snapshot)
	if err != nil {
		return model.ProductCreatePlan{}, "", err
	}
	approved, override, err := approvedProductBIOS(snapshot)
	if err != nil {
		return model.ProductCreatePlan{}, "", err
	}
	content, err := BuildProductContent(approved)
	if err != nil {
		return model.ProductCreatePlan{}, "", err
	}
	if service.blobs != nil {
		for _, check := range content.Checks {
			if err := service.blobs.Verify(ctx, check); err != nil {
				return model.ProductCreatePlan{}, "", fmt.Errorf("verify product content: %w", err)
			}
		}
	}
	initialDisc, err := productInitialDisc(snapshot, len(content.Discs))
	if err != nil {
		return model.ProductCreatePlan{}, "", err
	}
	external, err := productExternalFiles(approved, content)
	if err != nil {
		return model.ProductCreatePlan{}, "", err
	}
	id, err := checkedProductID(service.environment.NewID)
	if err != nil {
		return model.ProductCreatePlan{}, "", err
	}
	capability, hash, err := service.environment.SignCapability(id)
	if err != nil {
		return model.ProductCreatePlan{}, "", fmt.Errorf("sign product capability: %w", err)
	}
	plan := model.ProductCreatePlan{
		Command: command, Source: approved.Source, Content: content, External: external, ID: id,
		CredentialHash: hash, SelectedDOSEntry: selectedDOS.Path, InitialDiscIndex: initialDisc, OverrideBIOS: override,
	}
	if snapshot.Source.DeliveryProfile == "ISOLATED_WEB_PROJECT" {
		if service.environment.SignIsolation == nil {
			return model.ProductCreatePlan{}, "", model.ErrBlocked
		}
		ticket, err := service.environment.SignIsolation(id)
		if err != nil {
			return model.ProductCreatePlan{}, "", fmt.Errorf("sign product isolation: %w", err)
		}
		plan.Isolation = &ticket
	}
	return plan, capability, nil
}
