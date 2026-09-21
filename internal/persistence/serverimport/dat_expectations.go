package serverimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/firmware"
)

func (repository *Recovery) DATEntries(
	ctx context.Context,
	versionID, machineName string,
) ([]firmware.ExpectedDATEntry, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT entry.name,entry.size_bytes,entry.crc32,entry.sha1
FROM dat_rom_entries entry
WHERE entry.dat_version_id=? AND entry.machine_name=? AND COALESCE(entry.status,'GOOD')<>'NODUMP'
AND (entry.bios_name IS NULL OR EXISTS(
 SELECT 1 FROM dat_bios_sets bios WHERE bios.dat_version_id=entry.dat_version_id
 AND bios.machine_name=entry.machine_name AND bios.bios_name=entry.bios_name AND bios.is_default=1
)) ORDER BY entry.name COLLATE BINARY,entry.ordinal
`, versionID, machineName)
	if err != nil {
		return nil, fmt.Errorf("query expected DAT entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]firmware.ExpectedDATEntry, 0)
	for rows.Next() {
		var entry firmware.ExpectedDATEntry
		var crc, sha sql.NullString
		if err := rows.Scan(&entry.Name, &entry.SizeBytes, &crc, &sha); err != nil {
			return nil, fmt.Errorf("scan expected DAT entry: %w", err)
		}
		if crc.Valid {
			entry.CRC32 = crc.String
		}
		if sha.Valid {
			entry.SHA1 = sha.String
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expected DAT entries: %w", err)
	}
	return result, nil
}
