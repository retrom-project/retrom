package architecture

import "testing"

func TestCoreValidationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/corevalidation")
	assertBusinessImports(t, "../../capability/content/corevalidation")
	assertBusinessImports(t, "../../capability/runtime/runtimecatalog")
}
