package pegasusimport

import (
	"context"
	"errors"
	"testing"
)

type serviceCommands struct {
	failure error
	queued  bool
	calls   []string
}

func (fake *serviceCommands) Create(context.Context, CreateRequest, string) (Summary, error) {
	fake.calls = append(fake.calls, "create")
	return Summary{ID: "import"}, fake.failure
}

func (fake *serviceCommands) Start(context.Context, string, int64, string) (Summary, bool, error) {
	fake.calls = append(fake.calls, "start")
	return Summary{ID: "import"}, fake.queued, fake.failure
}
func (fake *serviceCommands) Signal() { fake.calls = append(fake.calls, "signal") }
func (fake *serviceCommands) Close()  { fake.calls = append(fake.calls, "close") }
func TestServiceSignalsOnlyCommittedNewWork(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"create failed", "create committed", "start failed", "start replay", "start committed"} {
		t.Run(name, func(t *testing.T) {
			failure := errors.New("storage unavailable")
			fake := &serviceCommands{queued: true}
			if name == "create failed" || name == "start failed" {
				fake.failure = failure
			}
			if name == "start replay" {
				fake.queued = false
			}
			service := New(ServiceDependencies{Creation: fake, Starter: fake, Worker: serviceWake{fake}})
			var err error
			if name == "create failed" || name == "create committed" {
				_, err = service.Create(t.Context(), CreateRequest{}, "actor")
			} else {
				_, err = service.StartImport(t.Context(), "import", 1, "actor")
			}
			if !errors.Is(err, fake.failure) {
				t.Fatalf("lost error: %v", err)
			}
			expected := 1
			if fake.failure == nil && fake.queued {
				expected = 2
			}
			if len(fake.calls) != expected || expected == 2 && fake.calls[1] != "signal" {
				t.Fatalf("effects=%v", fake.calls)
			}
		})
	}
}

type serviceWake struct{ *serviceCommands }

func (fake serviceWake) Start() { fake.calls = append(fake.calls, "worker start") }
