package tagging

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestTaggingUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
