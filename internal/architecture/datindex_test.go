package architecture

import "testing"

func TestDATIndexRulesUseBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/datindex")
}
