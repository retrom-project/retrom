package datindex

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestDATIndexRulesUseBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
