package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/filestore"
	launch "retrom/internal/service/launch"
)

func WithPreviewFiles(environment launch.PreviewEnvironment, files *filestore.Store) launch.PreviewEnvironment {
	environment.CopyRestorePayload = func(ctx context.Context, id, record string) (string, error) {
		if files == nil {
			return "", launch.ErrSaveIncompatible
		}
		complete := false
		defer func() {
			if !complete {
				cleanup.Error("discard incomplete preview restore", files.RemovePath(context.WithoutCancel(ctx), "previews/"+id))
			}
		}()
		copied, err := files.CopyTo(ctx, record, "previews/"+id+"/restore", "payload")
		if err != nil {
			return "", fmt.Errorf("copy isolated preview restore: %w", err)
		}
		complete = true
		return copied.Record, nil
	}
	environment.DiscardPreviewPayload = func(ctx context.Context, id string) error {
		if err := files.RemovePath(ctx, "previews/"+id); err != nil {
			return fmt.Errorf("discard preview restore: %w", err)
		}
		return nil
	}
	return environment
}
