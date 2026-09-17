package serverimport

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	model "retrom/internal/model/serverimport"
)

type Control struct {
	repository model.ControlRepository
	roots      map[string]string
	now        func() time.Time
}

func NewControl(
	repository model.ControlRepository,
	roots map[string]string,
	now func() time.Time,
) *Control {
	return &Control{
		repository: repository, roots: maps.Clone(roots), now: now,
	}
}

func (service *Control) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actorID string,
) (model.Summary, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return model.Summary{}, false, model.ErrNotCancellable
	}
	result, err := service.repository.CommitCancel(ctx, model.CancelCommand{
		ID: id, Version: version, Reason: reason,
		ActorID: actorID, Now: service.now().UnixMilli(),
	})
	if err != nil {
		return model.Summary{}, false,
			fmt.Errorf("commit server import cancellation: %w", err)
	}
	return result.Summary, result.Pending, nil
}

func (service *Control) Retry(
	ctx context.Context, id string, version int64, actorID string,
) (model.Summary, error) {
	result, err := service.repository.CommitRetry(ctx, model.RetryCommand{
		ID: id, Version: version, ActorID: actorID,
		Now: service.now().UnixMilli(), ValidRoots: service.roots,
	})
	if err != nil {
		return model.Summary{},
			fmt.Errorf("commit server import retry: %w", err)
	}
	return result, nil
}
