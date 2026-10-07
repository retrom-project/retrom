package library

import (
	"context"
	"encoding/json"
	"log/slog"

	"retrom/internal/model"
)

func (s *Service) ScummvmCandidates(ctx context.Context, p model.Principal, id string) (json.RawMessage, error) {
	if err := p.Admin(); err != nil {
		return nil, wrap(err)
	}
	detail, err := s.Repository.GameDetail(ctx, p.User.ID, id, "pending_review")
	if err != nil {
		detail, err = s.Repository.GameDetail(ctx, p.User.ID, id, "published")
	}
	if err != nil {
		return nil, wrap(err)
	}
	root := ""
	for _, binding := range s.Runtime.Bindings {
		if binding.CoreID == "scummvm" {
			root = s.Runtime.Providers[binding.ProviderID].Root
			break
		}
	}
	if root == "" {
		return nil, model.ErrUnavailable
	}
	key, tree, err := s.Storage.StageTree(ctx, detail.Files)
	if key != "" {
		defer func() {
			if removeErr := s.Storage.RemoveTemporary(key); removeErr != nil {
				logTemporary(removeErr)
			}
		}()
	}
	if err != nil {
		return nil, wrap(err)
	}
	var result json.RawMessage
	err = s.Runtime.Call(ctx, "detect-scummvm", map[string]any{"providerRoot": root, "treeRoot": tree}, &result)
	return result, wrap(err)
}

func logTemporary(err error) { slog.Error("remove game inspection files", "error", err) }
