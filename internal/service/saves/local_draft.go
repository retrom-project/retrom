package saves

import (
	"context"
)

// CreateLocalDraft checks current account ownership independently of expired runtime credentials.
func (service *Service) CreateLocalDraft(ctx context.Context, id, userID, profileID, key string,
	request ManualUpload,
) (ManualResult, bool, error) {
	launch, err := service.loadLaunch(ctx, id)
	if err != nil {
		return ManualResult{}, false, err
	}
	if launch.PrincipalID != userID || launch.ProfileID != profileID || !localDraftWritable(launch) {
		return ManualResult{}, false, ErrCredential
	}
	launch.localDraft = true
	return service.createManualForLaunch(ctx, id, key, request, launch)
}
