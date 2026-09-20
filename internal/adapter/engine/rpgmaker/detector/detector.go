// Package detector obtains bounded RPG Maker probe inputs for the pure rules.
package detector

import (
	"io"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

type fileIndex interface {
	Files() []policy.File
	Open(string) (io.ReadCloser, error)
}

func detect(coreID string, source fileIndex) (policy.Profile, error) {
	inspection, err := policy.Begin(coreID)
	if err != nil {
		// Preserve Begin's exact domain error; the selected core is already classified.
		return policy.Profile{}, err
	}
	facts := policy.CatalogFacts{Present: source != nil}
	if source != nil {
		facts.Files = source.Files()
	}
	if err := inspection.Index(facts); err != nil {
		// Index already assigns the catalog failure code and expected generation.
		return policy.Profile{}, err
	}
	for {
		probe, ready, err := inspection.Next()
		if err != nil {
			// Next already assigns the probe path, failure code and expected generation.
			return policy.Profile{}, err
		}
		if !ready {
			// Result owns the exact final ambiguity or generation-mismatch domain error.
			return inspection.Result()
		}
		contents, phase, cause := readProbe(probe, source)
		if phase != 0 {
			// Failure is the sole domain wrapper and retains the original I/O cause.
			return policy.Profile{}, inspection.Failure(phase, cause)
		}
		if err := inspection.Accept(contents); err != nil {
			// Accept already classifies size and parse failures with the expected generation.
			return policy.Profile{}, err
		}
	}
}
