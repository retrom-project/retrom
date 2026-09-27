package cleanup

import (
	"testing"

	"retrom/internal/testsupport/archcheck"
)

func TestPackageUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	archcheck.AssertBusinessImports(t)
}
