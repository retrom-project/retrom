package saves

import (
	"context"
	model "retrom/internal/model/saves"
)

// CreateLocalDraft checks current account ownership independently of expired runtime credentials.
func (service *Service) CreateLocalDraft(ctx context.Context, id, userID, profileID, key string,
	request ManualUpload,
) (model.ManualResult, bool, error) {
	launch, err := service.loadLaunch(ctx, id)
	if err != nil {
		return model.ManualResult{}, false, err
	}
	if launch.PrincipalID != userID || launch.ProfileID != profileID || !localDraftWritable(launch) {
		return model.ManualResult{}, false, model.ErrCredential
	}
	launch.LocalDraft = true
	return service.createManualForLaunch(ctx, id, key, request, launch)
}
