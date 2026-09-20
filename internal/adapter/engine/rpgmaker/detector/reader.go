package detector

import (
	"io"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

func readProbe(probe policy.Probe, source fileIndex) ([]byte, policy.ProbeFailurePhase, error) {
	reader, err := source.Open(probe.File.Path)
	if err != nil {
		// Inspection.Failure supplies the single domain wrapper for this original Open cause.
		return nil, policy.OpenFailure, err
	}
	if reader == nil {
		return nil, policy.MissingReader, nil
	}
	contents, readErr := io.ReadAll(io.LimitReader(reader, probe.MaxBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		// Preserve the Read cause for Inspection.Failure and its priority over Close failure.
		return nil, policy.ReadFailure, readErr
	}
	if closeErr != nil {
		// Inspection.Failure supplies the single domain wrapper for this original Close cause.
		return nil, policy.CloseFailure, closeErr
	}
	return contents, 0, nil
}
