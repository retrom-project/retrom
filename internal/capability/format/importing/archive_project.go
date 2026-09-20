package importing

import (
	"errors"
	"fmt"
)

// ArchiveMemberHeader is the pure evidence available before member bytes are consumed.
type ArchiveMemberHeader struct {
	Entry    ArchiveEntry
	Unpacked bool
}

// ProjectArchiveStageError preserves each format's original consumer failure boundary.
func ProjectArchiveStageError(header ArchiveMemberHeader, stageErr, closeErr error) error {
	if header.Entry.ArchiveFormat == "ZIP" {
		return fmt.Errorf("scan archive entry %q: %w", header.Entry.NormalizedPath, stageErr)
	}
	if header.Unpacked {
		return errors.Join(stageErr, closeErr, ErrElectronASARInvalid)
	}
	return stageErr
}
