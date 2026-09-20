package detector

import (
	"testing"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

func replayBound(t *testing.T, want capturedObservation, bound capturedBound) capturedObservation {
	t.Helper()
	index := newTraceIndex(repeatedIndex{name: bound.Path, declared: bound.Declared, actual: bound.Actual})
	declared := index.Files()[0]
	probe, err := prepareBoundedProbe(t, bound, declared)
	var contents []byte
	if err == nil {
		var phase policy.ProbeFailurePhase
		contents, phase, err = readProbe(probe, index)
		if phase != 0 {
			err = probe.Failure(phase, err)
		}
		if err == nil {
			err = probe.ValidateSize(len(contents))
		}
		if err != nil {
			contents = nil
		}
	}
	return observe(t, want, policy.Profile{}, err, index, contents)
}

func prepareBoundedProbe(t *testing.T, bound capturedBound, declared policy.File) (policy.Probe, error) {
	t.Helper()
	source := boundedPreludeSource(t, bound.Path)
	files := source.Files()
	found := false
	for position := range files {
		if files[position].Path == bound.Path {
			files[position] = declared
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture has no target probe %q", bound.Path)
	}
	inspection, err := policy.Begin(policy.VirtualCoreID)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspection.Index(policy.CatalogFacts{Present: true, Files: files}); err != nil {
		t.Fatal(err)
	}
	for {
		probe, ready, err := inspection.Next()
		if err != nil {
			return policy.Probe{}, err
		}
		if !ready {
			t.Fatalf("inspection completed before %q", bound.Path)
		}
		if probe.File.Path == bound.Path {
			if probe.MaxBytes != bound.Limit || probe.Code != bound.Code {
				t.Fatalf("probe contract = %#v, old limit/code = %d/%s", probe, bound.Limit, bound.Code)
			}
			return probe, nil
		}
		contents, phase, cause := readProbe(probe, source)
		if phase != 0 {
			t.Fatal(inspection.Failure(phase, cause))
		}
		if err := inspection.Accept(contents); err != nil {
			t.Fatal(err)
		}
	}
}

func boundedPreludeSource(t *testing.T, path string) fileIndex {
	t.Helper()
	var name string
	switch path {
	case "RPG_RT.ldb", "RPG_RT.lmt":
		name = "io/read"
	case "RPG_RT.ini":
		name = "ini-size/65535"
	case "Game.ini":
		name = "shape/rgss-library-conflict"
	case "data/System.json", "index.html", "js/main.js":
		name = "public/malicious-rpgmv"
	default:
		t.Fatalf("unregistered old probe %q", path)
	}
	return capturedSource(t, namedInput(t, name))
}
