package saves

import (
	"io"
	"time"

	"retrom/internal/adapter/files/blobstore"
	model "retrom/internal/model/saves"
)

const (
	MaxRequestBytes    = int64(270 << 20)
	maxScreenshotBytes = int64(10 << 20)
	maxPixels          = int64(40_000_000)
)

type Service struct {
	repository model.Repository
	blobs      *blobstore.Store
	now        func() time.Time
}

func New(repository model.Repository, blobs *blobstore.Store, now func() time.Time) *Service {
	return &Service{repository: repository, blobs: blobs, now: now}
}

// ManualUpload contains only the streamed multipart input needed by the save use case.
type ManualUpload struct {
	ContentType string
	Body        io.Reader
}

type CheckpointStatus struct {
	CheckpointFormat string                 `json:"checkpointFormat"`
	Availability     CheckpointAvailability `json:"availability"`
}

type CheckpointAvailability struct {
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
}
