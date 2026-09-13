package saves

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestPersistenceSaveCodeTreatsProviderCheckpointsAsOpaque(t *testing.T) {
	t.Parallel()
	architecture.AssertOpaqueProviderCheckpoints(t)
}
