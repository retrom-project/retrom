package launch

import (
	"fmt"
	"strings"

	"retrom/internal/capability/runtime/runtimebundle"
	"retrom/internal/capability/runtime/runtimecatalog"
	"retrom/internal/capability/runtime/runtimeoptions"
)

func providerWarnings(source ConfigSource) []string {
	warnings := make([]string, 0, 2)
	if strings.Contains(source.DependencyJSON, `"installationStatus":"HASH_WARNING"`) {
		warnings = append(warnings, "BIOS_HASH_WARNING")
	}
	if strings.Contains(source.DependencyJSON, `"installationStatus":"MISSING_ENTRY"`) ||
		strings.Contains(source.DependencyJSON, `.zip:MISSING_ENTRY"`) {
		warnings = append(warnings, "BIOS_MISSING_ENTRY_WARNING")
	}
	if source.Compatibility == "REVIEW_SCREENSHOT_OVERRIDE" {
		warnings = append(warnings, "REVIEW_SCREENSHOT_OVERRIDE")
	}
	return warnings
}

func providerTargetOptions(
	schema runtimebundle.TargetOptionsSchema,
	source ConfigSource,
) (map[string]any, error) {
	selected, registered := runtimecatalog.Strategy(source.DetectorProfile)
	if !registered {
		return nil, runtimeoptions.ErrUnsupported
	}
	dos := source.DOSEntry
	options, err := runtimeoptions.Build(selected.Options, schema, runtimeoptions.Input{
		DOSEntry: dos, ContentKind: source.ContentKind, InitialDiscIndex: source.InitialDisc,
		DependencySnapshot: source.DependencyJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble Host launch options: %w", err)
	}
	return options, nil
}
