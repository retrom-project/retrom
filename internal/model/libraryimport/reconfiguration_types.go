package libraryimport

import "context"

type ReconfigurationSource struct {
	SourceType string
	Files      []PreparedReusableUploadFile
}

type ReconfigurationClone struct {
	UploadID          string
	SourceImportJobID string
	ExpectedVersion   int64
	SourceType        string
	Files             []PreparedReusableUploadFile
	ManifestDigest    string
	NowMS             int64
}

type ReconfigurationRepository interface {
	Source(context.Context, string, int64) (ReconfigurationSource, bool, error)
	Clone(context.Context, ReconfigurationClone) error
	RemoveUnused(context.Context, string) error
}
