package gamecontent

import (
	"testing"
)

func TestConstructorRejectsMissingDependencies(t *testing.T) {
	for _, deps := range []Dependencies{{}, {Files: constructorFiles(t)}} {
		t.Run("missing dependency", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing required dependency accepted")
				}
			}()
			New(deps, Options{})
		})
	}
}
