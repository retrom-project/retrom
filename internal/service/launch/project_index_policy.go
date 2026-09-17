package launch

import (
	"fmt"
	model "retrom/internal/model/launch"

	butterscotch "retrom/internal/capability/engine/butterscotch/detector"
	kirikiri "retrom/internal/capability/engine/kirikiri/detector"
	nxengine "retrom/internal/capability/engine/nxengine/detector"
	ons "retrom/internal/capability/engine/ons/detector"
	"retrom/internal/capability/engine/scummvm"
)

type projectIndexPolicy struct {
	marker, font              string
	maximum, minimum          int
	allowEmpty, firstIsMarker bool
}

func projectIndexPolicyFor(format, raw string) (projectIndexPolicy, error) {
	policy := projectIndexPolicy{minimum: 1, maximum: 10_000}
	var err error
	switch format {
	case "ONS_PROJECT":
		var profile ons.Profile
		profile, err = ons.ParseSnapshot(raw)
		policy.marker, policy.font = profile.MarkerPath, profile.FontPath
		policy.minimum, policy.maximum = 2, MaximumProjectFiles
	case "KIRIKIRI_PROJECT":
		var profile kirikiri.Profile
		profile, err = kirikiri.ParseSnapshot(raw)
		policy.marker, policy.allowEmpty = profile.MarkerPath, true
	case "BUTTERSCOTCH_PROJECT":
		var profile butterscotch.Profile
		profile, err = butterscotch.ParseSnapshot(raw)
		policy.marker = profile.MarkerPath
	case "NXENGINE_PROJECT":
		var profile nxengine.Profile
		profile, err = nxengine.ParseSnapshot(raw)
		policy.marker, policy.maximum = profile.MarkerPath, 4096
	case "SCUMMVM_PROJECT":
		err = validateScummVMIndex(raw)
		policy.allowEmpty, policy.firstIsMarker = true, true
	default:
		return projectIndexPolicy{}, model.ErrProjectIndexUnavailable
	}
	if err != nil {
		return projectIndexPolicy{}, fmt.Errorf("%w: project profile: %w", model.ErrCredential, err)
	}
	return policy, nil
}

func validateScummVMIndex(raw string) error {
	snapshot, err := scummvm.ParseSnapshot(raw)
	if err != nil {
		return fmt.Errorf("parse ScummVM index: %w", err)
	}
	if _, err := snapshot.Selected(); err != nil {
		return fmt.Errorf("select ScummVM index: %w", err)
	}
	return nil
}
