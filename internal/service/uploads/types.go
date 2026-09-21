package uploads

import (
	"errors"
)

const PartSize = int64(8 << 20)

var ErrInvalid = errors.New("UPLOAD_INVALID")

type Canceled struct {
	UploadID string `json:"uploadId"`
	State    string `json:"state"`
	Version  int64  `json:"version"`
}

type FileDeclaration struct {
	ClientFileID string `json:"clientFileId"`
	RelativePath string `json:"relativePath"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type CreateRequest struct {
	Purpose    string            `json:"purpose,omitempty"`
	SourceType string            `json:"sourceType"`
	Files      []FileDeclaration `json:"files"`
}

type File struct {
	ID           string `json:"fileId"`
	ClientFileID string `json:"clientFileId,omitempty"`
	RelativePath string `json:"relativePath"`
	SizeBytes    int64  `json:"sizeBytes"`
	Received     int64  `json:"receivedSizeBytes"`
	State        string `json:"state"`
	Parts        []int  `json:"receivedParts"`
}

type Session struct {
	ID             string  `json:"uploadId"`
	State          string  `json:"state"`
	Purpose        string  `json:"purpose"`
	SourceType     string  `json:"sourceType"`
	TotalBytes     int64   `json:"totalBytes"`
	FinalizationNo int64   `json:"finalizationNo"`
	FinalizeJobID  *string `json:"finalizeJobId"`
	Version        int64   `json:"version"`
	ExpiresAtMS    int64   `json:"expiresAtMs"`
	ChunkSizeBytes int64   `json:"chunkSizeBytes"`
	Files          []File  `json:"files"`
}

var (
	ErrNotFound    = errors.New("UPLOAD_NOT_FOUND")
	errFinalizeIO  = errors.New("UPLOAD_FINALIZE_IO")
	errPartMissing = errors.New("UPLOAD_PART_MISSING")
	errPartCorrupt = errors.New("UPLOAD_PART_CORRUPT")
)

type byteRange struct{ start, end, total int64 }
