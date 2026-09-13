package architecture

import "testing"

func TestImmersiveUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/immersive")
}
