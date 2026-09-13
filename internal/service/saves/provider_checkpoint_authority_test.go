package saves

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestSaveProductionCodeTreatsProviderCheckpointsAsOpaque(t *testing.T) {
	architecture.AssertOpaqueProviderCheckpoints(t)
}
