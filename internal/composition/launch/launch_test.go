package launch_test

import (
	"testing"
	"time"

	variantcomposition "retrom/internal/composition/gamevariant"
	"retrom/internal/launch"
)

func TestAssemblyCanCloseWithoutDispatchingValidation(t *testing.T) {
	service := variantcomposition.New(nil, launch.NewSources(nil, nil), func() time.Time { return time.UnixMilli(1000) }, nil)
	service.Close()
	service.Resume(t.Context(), "after-close")
	if err := service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
}
