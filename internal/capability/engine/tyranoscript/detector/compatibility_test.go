package detector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestDetectOldGoValueCompatibility(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("testdata/old-values/old-go-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var observations []struct {
		Name      string
		Files     []File
		Profile   Profile
		Error     string
		ErrorType string
		Invalid   bool
		Snapshot  string
	}
	if err := json.Unmarshal(contents, &observations); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 24 {
		t.Fatalf("old-Go observations=%d, want 24", len(observations))
	}
	for _, observation := range observations {
		t.Run(observation.Name, func(t *testing.T) {
			t.Parallel()
			profile, err := Detect(observation.Files)
			message, kind := "", ""
			if err != nil {
				message, kind = err.Error(), fmt.Sprintf("%T", err)
			}
			if profile != observation.Profile || message != observation.Error || kind != observation.ErrorType ||
				errors.Is(err, ErrProjectInvalid) != observation.Invalid {
				t.Fatalf("profile=%#v error=%q type=%q invalid=%v; old=%#v",
					profile, message, kind, errors.Is(err, ErrProjectInvalid), observation)
			}
			if err == nil {
				snapshot, marshalErr := MarshalSnapshot(profile)
				if marshalErr != nil || string(snapshot) != observation.Snapshot {
					t.Fatalf("snapshot=%q error=%v; old=%q", snapshot, marshalErr, observation.Snapshot)
				}
			}
		})
	}
}
