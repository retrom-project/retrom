package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type frozenSourceRecords struct{ executor dbexec.Executor }

func (records frozenSourceRecords) Read(ctx context.Context, id string) (application.FrozenSourceSnapshot, error) {
	var result application.FrozenSourceSnapshot
	err := records.executor.QueryRowContext(ctx, `
SELECT root_config_digest,COALESCE(source_snapshot_digest,''),release_year_max
FROM emulationstation_imports WHERE id=?`, id).Scan(
		&result.RootConfigDigest,
		&result.SourceSnapshotDigest,
		&result.ReleaseYearMax,
	)
	if err != nil {
		return application.FrozenSourceSnapshot{}, fmt.Errorf("read EmulationStation frozen source: %w", err)
	}
	result.Gamelists, err = records.gamelists(ctx, id)
	if err != nil {
		return application.FrozenSourceSnapshot{}, err
	}
	return result, nil
}

func (records frozenSourceRecords) gamelists(ctx context.Context, id string) ([]application.GamelistEvidence, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT relative_path,size_bytes,content_digest,source_facts_digest,parse_state FROM emulationstation_import_gamelists
WHERE import_id=? ORDER BY relative_path LIMIT ?`, id, application.MaxSnapshotGamelists+1)
	if err != nil {
		return nil, fmt.Errorf("read EmulationStation metadata evidence: %w", err)
	}
	defer func() { cleanup.Error("close EmulationStation metadata evidence", rows.Close()) }()
	result := []application.GamelistEvidence{}
	for rows.Next() {
		var value application.GamelistEvidence
		if err := rows.Scan(
			&value.RelativePath, &value.SizeBytes, &value.ContentDigest, &value.FactsDigest, &value.ParseState,
		); err != nil {
			return nil, fmt.Errorf("scan EmulationStation metadata evidence: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate EmulationStation metadata evidence: %w", err)
	}
	return result, nil
}
