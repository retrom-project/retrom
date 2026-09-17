package libraryimport

import (
	"context"
	"errors"
	model "retrom/internal/model/libraryimport"
	"time"

	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	"retrom/internal/capability/format/importing"
)

var ErrImportWorkerClosed = errors.New("import worker is closed")

func ImportFailure(cause error) (string, bool) {
	if errors.Is(cause, context.DeadlineExceeded) {
		return "IMPORT_GROUP_EXECUTION_TIMEOUT", true
	}
	var detection *detector.Error
	if errors.As(cause, &detection) {
		return string(detection.Code), false
	}
	var project *fileset.ProjectError
	if errors.As(cause, &project) {
		return string(project.Code), false
	}
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{importing.ErrArchiveLimitExceeded, "ARCHIVE_LIMIT_EXCEEDED"},
		{importing.ErrArchiveEncrypted, "ARCHIVE_ENCRYPTED_UNSUPPORTED"},
		{importing.ErrArchiveVolumeUnsupported, "ARCHIVE_VOLUME_UNSUPPORTED"},
		{importing.ErrArchiveCasefoldCollision, "RPG_PATH_COLLISION"},
		{importing.ErrNWJSExecutableInvalid, "ARCHIVE_UNSAFE"},
		{model.ErrMultiDiscPlaylistMissing, "MULTI_DISC_PLAYLIST_MISSING"},
		{model.ErrMultiDiscModeUnavailable, "MULTI_DISC_MODE_UNAVAILABLE"},
		{model.ErrInvalid, "IMPORT_INPUT_INVALID"},
	} {
		if errors.Is(cause, candidate.err) {
			return candidate.code, false
		}
	}
	return "IMPORT_GROUP_FAILED", true
}

func importRetryDelay(attempt int64) time.Duration {
	if attempt == 1 {
		return time.Second
	}
	if attempt == 2 {
		return 5 * time.Second
	}
	return 30 * time.Second
}
