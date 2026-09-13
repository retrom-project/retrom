package blobstore

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestBlobStoreUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
