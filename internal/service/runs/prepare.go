package runs

import (
	"context"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func (s *Service) prepare(ctx context.Context, p model.Principal, input model.RunInput,
	detail model.GameDetail, directory model.Directory,
) (runtimeclient.Prepared, *model.Save, error) {
	var save *model.Save
	var savedContext *model.Extinfo
	if input.SaveID != "" {
		if input.Purpose != "play" {
			return runtimeclient.Prepared{}, nil, model.ErrInvalid
		}
		stored, err := s.Repository.Save(ctx, p.User.ID, input.SaveID)
		if err != nil {
			return runtimeclient.Prepared{}, nil, wrap(err)
		}
		if stored.Game.ID != detail.Game.ID {
			return runtimeclient.Prepared{}, nil, model.ErrInvalid
		}
		save, savedContext = &stored, &stored.Extinfo
		input.CoreID = stored.Extinfo.CoreID
	}
	prepared, err := s.Runtime.Prepare(ctx, directory, detail.RuntimeConfig, detail.Files, input.CoreID, savedContext)
	return prepared, save, wrap(err)
}
