package launch_test

import (
	"testing"
	"time"

	composition "retrom/internal/composition/launch"
	"retrom/internal/launch"
)

func TestAssemblyCanCloseWithoutDispatchingValidation(t *testing.T) {
	service := composition.New(nil, launch.NewSources(nil, nil), "", func() time.Time { return time.UnixMilli(1000) })
	service.Close()
	service.ResumeValidationJob(t.Context(), "after-close")
	service.ResumeQueuedValidationJobs()
}
