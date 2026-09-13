package uploads

import (
	"database/sql"
	"fmt"

	service "retrom/internal/service/uploads"
)

func scanFinalizationManifest(rows *sql.Rows) ([]service.FrozenFile, error) {
	files := []service.FrozenFile{}
	for rows.Next() {
		var id string
		var size int64
		var number, offset, length sql.NullInt64
		var sha, path sql.NullString
		if err := rows.Scan(&id, &size, &number, &offset, &length, &sha, &path); err != nil {
			return nil, fmt.Errorf("scan finalization manifest: %w", err)
		}
		if len(files) == 0 || files[len(files)-1].ID != id {
			files = append(files, service.FrozenFile{ID: id, Size: size, Parts: []service.Part{}})
		}
		if number.Valid {
			file := &files[len(files)-1]
			file.Parts = append(file.Parts, service.Part{
				Number: int(number.Int64), Offset: offset.Int64, Size: length.Int64, SHA256: sha.String, Path: path.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finalization manifest: %w", err)
	}
	return files, nil
}
