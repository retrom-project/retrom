package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	service "retrom/internal/service/uploads"
)

func (repository *Repository) Snapshot(ctx context.Context, id string) (service.Session, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return service.Session{}, fmt.Errorf("uploads/begin snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	var result service.Session
	var job sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT id,state,purpose,source_type,total_bytes,finalization_no,finalize_job_id,version,expires_at_ms
FROM upload_sessions WHERE id=?
`, id).Scan(&result.ID, &result.State, &result.Purpose, &result.SourceType, &result.TotalBytes, &result.FinalizationNo,
		&job, &result.Version, &result.ExpiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return service.Session{}, service.ErrNotFound
	}
	if err != nil {
		return service.Session{}, fmt.Errorf("uploads/read snapshot: %w", err)
	}
	result.FinalizeJobID = dbexec.StringPointer(job)
	result.Files, err = snapshotFiles(ctx, tx, id)
	if err != nil {
		return service.Session{}, err
	}
	if err := snapshotParts(ctx, tx, id, result.Files); err != nil {
		return service.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.Session{}, fmt.Errorf("uploads/commit snapshot: %w", err)
	}
	return result, nil
}

func snapshotFiles(ctx context.Context, executor dbexec.Executor, id string) ([]service.File, error) {
	rows, err := executor.QueryContext(ctx, `
SELECT id,relative_path,declared_size_bytes,received_size_bytes,state FROM upload_files
WHERE upload_session_id=? ORDER BY relative_path,id
`, id)
	if err != nil {
		return nil, fmt.Errorf("uploads/read files: %w", err)
	}
	defer func() { cleanup.Error("close upload files", rows.Close()) }()
	files := make([]service.File, 0)
	for rows.Next() {
		var file service.File
		file.Parts = []int{}
		if err := rows.Scan(&file.ID, &file.RelativePath, &file.SizeBytes, &file.Received, &file.State); err != nil {
			return nil, fmt.Errorf("uploads/scan file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploads/iterate files: %w", err)
	}
	return files, nil
}

func snapshotParts(ctx context.Context, executor dbexec.Executor, id string, files []service.File) error {
	rows, err := executor.QueryContext(ctx, `
SELECT part.upload_file_id,part.part_no FROM upload_parts part
JOIN upload_files file ON file.id=part.upload_file_id WHERE file.upload_session_id=? ORDER BY
part.upload_file_id,part.part_no
`, id)
	if err != nil {
		return fmt.Errorf("uploads/read part bitmap: %w", err)
	}
	defer func() { cleanup.Error("close upload part bitmap", rows.Close()) }()
	indices := make(map[string]int, len(files))
	for index, file := range files {
		indices[file.ID] = index
	}
	for rows.Next() {
		var fileID string
		var number int
		if err := rows.Scan(&fileID, &number); err != nil {
			return fmt.Errorf("uploads/scan part bitmap: %w", err)
		}
		if index, exists := indices[fileID]; exists {
			files[index].Parts = append(files[index].Parts, number)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("uploads/iterate part bitmap: %w", err)
	}
	return nil
}

func (repository *Repository) Parts(ctx context.Context, id string) ([]service.Part, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT offset_bytes,size_bytes,storage_key,sha256,part_no FROM upload_parts WHERE upload_file_id=? ORDER BY
offset_bytes
`, id)
	if err != nil {
		return nil, fmt.Errorf("uploads/read file parts: %w", err)
	}
	defer func() { cleanup.Error("close upload parts", rows.Close()) }()
	parts := make([]service.Part, 0)
	for rows.Next() {
		var part service.Part
		if err := rows.Scan(&part.Offset, &part.Size, &part.Path, &part.SHA256, &part.Number); err != nil {
			return nil, fmt.Errorf("uploads/scan part: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploads/iterate parts: %w", err)
	}
	return parts, nil
}

func (repository *Repository) Candidates(ctx context.Context, id string) ([]service.Candidate, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT id,declared_size_bytes FROM upload_files WHERE upload_session_id=? AND state!='COMPLETE' ORDER BY id
`, id)
	if err != nil {
		return nil, fmt.Errorf("uploads/read finalize candidates: %w", err)
	}
	defer func() { cleanup.Error("close finalize candidates", rows.Close()) }()
	var files []service.Candidate
	for rows.Next() {
		var file service.Candidate
		if err := rows.Scan(&file.ID, &file.Size); err != nil {
			return nil, fmt.Errorf("uploads/scan finalize candidate: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploads/iterate finalize candidates: %w", err)
	}
	return files, nil
}
