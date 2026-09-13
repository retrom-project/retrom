package architecture

import "testing"

func TestTaggingUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/tagging")
}
