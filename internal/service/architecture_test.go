package service

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestServicesDependOnBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImportsTree(t)
}
