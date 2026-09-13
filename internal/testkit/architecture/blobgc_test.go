package architecture

import "testing"

func TestBlobGCDependsOnBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../../service/blobgc")
}
