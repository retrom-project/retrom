package cleanupjobs

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
)

func (service *DeletionScheduler) StageInScope(ctx context.Context, scope DeletionScope, records []string) error {
	seen := map[string]bool{}
	for _, value := range records {
		if value == "" {
			continue
		}
		directory, err := filestore.CleanupDirectory(value)
		if err != nil {
			return fmt.Errorf("stage in scope: %w", err)
		}
		if directory == "" || seen[directory] {
			continue
		}
		seen[directory] = true
		if err := scope.Write.QueuePath(ctx, directory, service.now().UnixMilli()); err != nil {
			return fmt.Errorf("queue replaced file removal: %w", err)
		}
	}
	return nil
}
