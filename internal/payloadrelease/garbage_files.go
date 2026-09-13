package payloadrelease

import (
	"retrom/internal/blobstore"
	"retrom/internal/payloadfiles"
)

// Sources is retained as a compatibility name for callers of the legacy
// payload package. The implementation lives in the storage adapter package.
type Sources = payloadfiles.Store

func NewSources(blobs *blobstore.Store) *Sources { return payloadfiles.New(blobs) }
