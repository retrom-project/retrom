package payloadrelease

import (
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/payloadfiles"
)

// Sources is retained as a compatibility name for callers of the legacy
// payload package. The implementation lives in the storage adapter package.
type Sources = payloadfiles.Store

func NewSources(blobs *blobstore.Store) *Sources { return payloadfiles.New(blobs) }
