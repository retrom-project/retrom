package architecture

import "testing"

func TestCoreValidationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/corevalidation")
	assertBusinessImports(t, "../core/validation")
	assertBusinessImports(t, "../runtime/catalog")
}
