package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	service "retrom/internal/service/uploads"
)

func (records partRecords) Put(ctx context.Context, input service.PartRecord) (bool, error) {
	part := input.Part
	result, err := records.executor.ExecContext(ctx, `
INSERT INTO upload_parts(upload_file_id,part_no,offset_bytes,size_bytes,sha256,storage_key,created_at_ms)
VALUES(?,?,?,?,?,?,?) ON CONFLICT(upload_file_id,part_no) DO NOTHING
`, input.FileID, part.Number, part.Offset, part.Size, part.SHA256, part.Path, input.AtMS)
	if err != nil {
		return false, fmt.Errorf("uploads/insert part: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("uploads/count inserted parts: %w", err)
	}
	return count == 1, nil
}

func (records partRecords) Get(ctx context.Context, id string, number int) (service.PartRecord, bool, error) {
	var result service.PartRecord
	result.FileID = id
	result.Part.Number = number
	err := records.executor.QueryRowContext(ctx, `
SELECT offset_bytes,size_bytes,sha256,storage_key,created_at_ms FROM upload_parts WHERE upload_file_id=? AND
part_no=?
`, id, number).Scan(&result.Part.Offset, &result.Part.Size, &result.Part.SHA256, &result.Part.Path, &result.AtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return service.PartRecord{}, false, nil
	}
	if err != nil {
		return service.PartRecord{}, false, fmt.Errorf("uploads/read part: %w", err)
	}
	return result, true, nil
}

func (records partRecords) DeleteForFile(ctx context.Context, id string) error {
	if _, err := records.executor.ExecContext(ctx, `DELETE FROM upload_parts WHERE upload_file_id=?`, id); err != nil {
		return fmt.Errorf("uploads/delete finalized parts: %w", err)
	}
	return nil
}
