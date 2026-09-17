package libraryimport

import (
	"time"

	model "retrom/internal/model/libraryimport"
)

type MultiDiscAttachmentCommits struct {
	repository model.MultiDiscAttachmentCommitRepository
	now        func() time.Time
	newID      func() (string, error)
}

func multiDiscAttachmentError(code string, cause error) error {
	return &model.MultiDiscAttachmentError{Code: code, Cause: cause}
}
