package emulationstationimport

import (
	"errors"
	"testing"
)

func TestChildListsRejectUnknownImport(t *testing.T) {
	t.Parallel()
	fixture := newLifecycleFixture(t)
	const missing = "01980000-0000-7000-8000-000000000899"
	checks := []struct {
		name string
		run  func() error
	}{
		{name: "gamelists", run: func() error {
			_, err := fixture.service.Gamelists(fixture.context, missing, "", "", 10)
			return err
		}},
		{name: "collections", run: func() error {
			_, err := fixture.service.Collections(fixture.context, missing, "", "", 10)
			return err
		}},
		{name: "items", run: func() error {
			_, err := fixture.service.Items(fixture.context, missing, "", "", "", "", "", "", 10)
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want %v", err, ErrNotFound)
			}
		})
	}
}
