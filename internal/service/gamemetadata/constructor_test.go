package gamemetadata

import (
	"testing"
)

func TestConstructorRejectsMissingDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("missing required dependency accepted")
		}
	}()
	New(nil, nil, nil)
}
