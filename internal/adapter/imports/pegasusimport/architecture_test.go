package pegasusimport

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestPegasusAdaptersDoNotConstructPersistence(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
