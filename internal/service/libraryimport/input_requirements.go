package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"

	"retrom/internal/content/diagnostic"
)

func (service *ImportPreparation) applyInputRequirements(ctx context.Context, plan *PreparedImport) error {
	policy := plan.Target.Policy.Requirements
	if policy == nil {
		return nil
	}
	return filterPreparedGroups(plan, func(group *PreparedGroup) (*diagnostic.Rejection, error) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare input requirements: %w", err)
		}
		if PreparedGroupContentKind(*group) != "SINGLE_FILE" || len(group.Sources) != 1 {
			return nil, ErrInvalid
		}
		source := group.Sources[0]
		record, err := preparedSourceRecord(source, plan.Archives)
		if err != nil {
			return nil, err
		}
		file, err := service.blobs.OpenRecord(record)
		if err != nil {
			return nil, fmt.Errorf("read input requirement content: %w", err)
		}
		defer func() { cleanup.Error("close", file.Close()) }()
		_, size, err := preparedSourceIdentity(source, plan.Archives)
		if err != nil {
			return nil, err
		}
		facts, rejection, err := policy.Inspect(ctx, file, size, source.LogicalName)
		if err != nil {
			return nil, fmt.Errorf("inspect input requirements: %w", err)
		}
		group.ContentFacts = facts
		return rejection, nil
	})
}

func preparedSourceRecord(source PreparedSource, archives []PreparedArchive) (string, error) {
	if source.Payload != nil {
		return source.Payload.Record, nil
	}
	if source.ArchiveOrdinal == nil {
		return source.File.FileRecord, nil
	}
	for _, archive := range archives {
		if archive.FileRecord == source.ArchiveFileRecord {
			if value, found := archive.Materialized[*source.ArchiveOrdinal]; found {
				return value.Record, nil
			}
		}
	}
	return "", ErrInvalid
}
