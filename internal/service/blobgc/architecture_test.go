package blobgc

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestBlobGCDependsOnBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
