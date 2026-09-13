package jobs

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestJobsDependOnBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
