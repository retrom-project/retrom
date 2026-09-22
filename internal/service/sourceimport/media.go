package sourceimport

import "strings"

func ProjectMedia(present bool, warnings []map[string]any, field string) string {
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
	if present {
		return "READY"
	}
	return "MISSING"
}
