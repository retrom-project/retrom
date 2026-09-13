package corevalidation

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestCoreValidationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
