package sourceimport

import "strings"

func ProjectMedia(state string, warnings []map[string]any, field string) string {
	if state == "RELEASED" {
		return "RELEASED"
	}
	for _, warning := range warnings {
		warningField, _ := warning["field"].(string)
		code, _ := warning["code"].(string)
		mediaWarning := strings.HasPrefix(code, "PEGASUS_IMAGE_") ||
			strings.HasPrefix(code, "PEGASUS_VIDEO_") ||
			strings.HasPrefix(code, "PEGASUS_MEDIA_")
		if warningField == field && mediaWarning {
			return "WARNING"
		}
	}
	switch state {
	case "COPIED":
		return "READY"
	case "DISCOVERED":
		return "PENDING"
	case "", "MISSING":
		return "MISSING"
	default:
		return "WARNING"
	}
}
