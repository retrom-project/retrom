package launch_test

import (
	"testing"
	"time"

	variantcomposition "retrom/internal/composition/gamevariant"
	"retrom/internal/launch"
)

func TestAssemblyCanCloseWithoutDispatchingValidation(t *testing.T) {
	service := variantcomposition.New(nil, launch.NewSources(nil, nil), func() time.Time { return time.UnixMilli(1000) })
	service.Close()
	service.Resume(t.Context(), "after-close")
	service.Recover()
}
