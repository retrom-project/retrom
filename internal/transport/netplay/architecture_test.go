package netplay

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestNetplayTransportDoesNotConstructPersistence(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
