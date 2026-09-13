package emulationstationimport

import "strings"

func ProjectMedia(present bool, warnings []map[string]any, field string) string {
	for _, warning := range warnings {
		warningField, _ := warning["field"].(string)
		pathKind, _ := warning["pathKind"].(string)
		code, _ := warning["code"].(string)
		mediaWarning := strings.HasPrefix(code, "EMULATIONSTATION_IMAGE_") ||
			strings.HasPrefix(code, "EMULATIONSTATION_VIDEO_") ||
			strings.HasPrefix(code, "EMULATIONSTATION_MEDIA_")
		matchesKind := (field == "cover" && pathKind == "COVER") ||
			(field == "video" && pathKind == "VIDEO")
		if (warningField == field || matchesKind) && mediaWarning {
			return "WARNING"
		}
	}
	if present {
		return "READY"
	}
	return "MISSING"
}
