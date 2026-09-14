package libraryimport

import (
	"time"
)

type MultiDiscAttachmentCommits struct {
	repository MultiDiscAttachmentCommitRepository
	now        func() time.Time
	newID      func() (string, error)
}

func multiDiscAttachmentError(code string, cause error) error {
	return &MultiDiscAttachmentError{Code: code, Cause: cause}
}
