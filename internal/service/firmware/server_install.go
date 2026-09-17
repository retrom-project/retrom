package firmware

import (
	"context"
	"fmt"
	"maps"
	model "retrom/internal/model/firmware"
)

func (service *Service) InstallServerCandidate(
	ctx context.Context,
	request model.ServerInstallRequest,
) (model.ServerInstallResult, error) {
	request.Details = maps.Clone(request.Details)
	if request.Details == nil {
		request.Details = map[string]any{}
	}
	result, err := service.repository.CommitServerInstall(ctx, model.ServerInstallCommand{
		Request: request,
		NowFn:   func() int64 { return service.now().UnixMilli() },
	})
	if err != nil {
		return model.ServerInstallResult{}, fmt.Errorf("install server BIOS: %w", err)
	}
	service.signalRelease()
	return result, nil
}
