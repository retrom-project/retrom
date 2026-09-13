package architecture

import "testing"

func TestJobsDependOnBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/jobs")
}
