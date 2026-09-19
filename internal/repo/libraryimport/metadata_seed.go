package libraryimport

import (
	"context"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

// SeedMetadata reads the current metadata draft, applies normalization and
// change detection, and writes the result if changed. It uses the caller's
// executor and does not manage its own transaction.
func SeedMetadata(
	ctx context.Context, executor dbexec.Executor, input application.MetadataSeedInput,
) (int64, []application.ServerMetadataWarning, error) {
	records := metadataRecords{executor: executor}
	before, err := records.CurrentMetadata(ctx, input.ItemID)
	if err != nil {
		return 0, nil, fmt.Errorf("read server review metadata: %w", err)
	}
	plan, warnings, err := application.BuildMetadataSeed(before, input)
	if err != nil {
		return 0, nil, err
	}
	if !plan.Changed {
		return plan.ResultVersion, warnings, nil
	}
	if err := records.SaveMetadata(ctx, plan.Change); err != nil {
		return 0, nil, fmt.Errorf("save server review metadata: %w", err)
	}
	return plan.ResultVersion, warnings, nil
}
