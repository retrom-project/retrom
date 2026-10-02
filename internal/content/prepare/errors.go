package prepare

import (
	"errors"

	"retrom/internal/importing"
)

func archiveError(err error) error {
	codes := []struct {
		cause error
		code  string
	}{
		{importing.ErrArchiveUnsafe, "ARCHIVE_UNSAFE"},
		{importing.ErrUnsafeLogicalPath, "ARCHIVE_UNSAFE"},
		{importing.ErrArchiveLimitExceeded, "ARCHIVE_LIMIT_EXCEEDED"},
		{importing.ErrArchiveEncrypted, "ARCHIVE_ENCRYPTED_UNSUPPORTED"},
		{importing.ErrArchiveVolumeUnsupported, "ARCHIVE_VOLUME_UNSUPPORTED"},
		{importing.ErrArchiveMethodUnsupported, "ARCHIVE_METHOD_UNSUPPORTED"},
		{importing.ErrArchiveCasefoldCollision, "ARCHIVE_CASEFOLD_COLLISION"},
		{importing.ErrNestedArchiveUnsupported, "NESTED_ARCHIVE_UNSUPPORTED"},
	}
	for _, item := range codes {
		if errors.Is(err, item.cause) {
			return &Invalid{Code: item.code}
		}
	}
	return err
}
