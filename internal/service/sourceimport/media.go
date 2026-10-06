package sourceimport

func ProjectMedia(state string, warnings []map[string]any, field string) string {
	if state == "RELEASED" {
		return "RELEASED"
	}
	for _, warning := range warnings {
		warningField, _ := warning["field"].(string)
		if warningField == field {
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
