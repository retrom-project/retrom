package libraryimport

import libraryservice "retrom/internal/service/libraryimport"

func preparedGroupContentKind(group preparedGroup) string {
	return libraryservice.PreparedGroupContentKind(group)
}
