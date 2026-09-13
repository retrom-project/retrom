package corevalidation

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestCoreValidationCapabilityUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
