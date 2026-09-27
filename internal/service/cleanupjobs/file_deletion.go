package cleanupjobs

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
)

type EffectAuthority interface {
	CheckInScope(context.Context, WorkerScope, Work) error
}
type FileDeletionFiles interface {
	Delete(context.Context, string) error
}
type FileDeletionCollector struct {
	repository WorkerRepository
	authority  EffectAuthority
	files      FileDeletionFiles
}

func NewFileDeletionCollector(repository WorkerRepository, authority EffectAuthority,
	files FileDeletionFiles,
) *FileDeletionCollector {
	return &FileDeletionCollector{repository: repository, authority: authority, files: files}
}

func (service *FileDeletionCollector) Execute(ctx context.Context, unit Execution) error {
	if unit.Input.Kind != "PATH_DELETE" || unit.Input.Scope != unit.Work.Scope ||
		unit.Work.Scope.Type != ScopePath || !filestore.RemovablePath(unit.Input.Inputs.RelativePath) {
		return ErrInputInvalid
	}
	if err := service.repository.WithWorker(ctx, func(scope WorkerScope) error {
		return service.authority.CheckInScope(ctx, scope, unit.Work)
	}); err != nil {
		return fmt.Errorf("authorize path removal: %w", err)
	}
	if err := service.files.Delete(ctx, unit.Input.Inputs.RelativePath); err != nil {
		return effectFailure("PATH_DELETE_IO_FAILED", err)
	}
	return nil
}
