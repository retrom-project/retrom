package launch

import (
	"context"
	"errors"
)

// ErrProjectIndexUnavailable permits the consumer to request a frozen static index.
var ErrProjectIndexUnavailable = errors.New("PROJECT_INDEX_UNAVAILABLE")

type ProjectIndexView struct {
	Contents []byte
	SHA256   string
}

type ProjectIndexReference struct {
	ID          string
	PreviewOnly bool
}

type ProjectIndexRecord struct {
	Content ConfigFile
	Order   int64
	Primary bool
}

type ProjectIndexSnapshot struct {
	Source ConfigSource
	Files  []ProjectIndexRecord
}

type ProjectIndexReader interface {
	// ReadProjectIndex authorizes the session before reading files in the same snapshot.
	ReadProjectIndex(context.Context, ProjectIndexReference, ConfigAuthorization) (ProjectIndexSnapshot, bool, error)
}
